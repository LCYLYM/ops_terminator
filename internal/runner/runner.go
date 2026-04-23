package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"osagentmvp/internal/models"
)

type StreamSink func(kind string, chunk string)

type Result struct {
	Command   string        `json:"command"`
	Stdout    string        `json:"stdout"`
	Stderr    string        `json:"stderr"`
	ExitCode  int           `json:"exit_code"`
	Duration  time.Duration `json:"duration"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at"`
}

type Executor struct {
	Timeout        time.Duration
	KnownHostsPath string
}

func NewExecutor(timeout time.Duration, knownHostsPath string) *Executor {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	return &Executor{Timeout: timeout, KnownHostsPath: knownHostsPath}
}

func (e *Executor) Run(ctx context.Context, host models.Host, command string, sink StreamSink) (Result, error) {
	runCtx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()

	switch host.Mode {
	case "", models.HostModeLocal:
		return e.runLocal(runCtx, command, sink)
	case models.HostModeSSH:
		return e.runSSH(runCtx, host, command, sink)
	default:
		return Result{}, fmt.Errorf("unsupported host mode: %s", host.Mode)
	}
}

func (e *Executor) runLocal(ctx context.Context, command string, sink StreamSink) (Result, error) {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", command)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, err
	}

	started := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	var stdoutBuf strings.Builder
	var stderrBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go streamPipe(stdout, "stdout", sink, &stdoutBuf, &wg)
	go streamPipe(stderr, "stderr", sink, &stderrBuf, &wg)
	waitErr := cmd.Wait()
	wg.Wait()

	result := Result{
		Command:   command,
		Stdout:    stdoutBuf.String(),
		Stderr:    stderrBuf.String(),
		ExitCode:  exitCode(waitErr),
		StartedAt: started,
		EndedAt:   time.Now().UTC(),
	}
	result.Duration = result.EndedAt.Sub(started)
	return result, wrapWaitError(waitErr)
}

func (e *Executor) runSSH(ctx context.Context, host models.Host, command string, sink StreamSink) (Result, error) {
	if host.Address == "" || host.User == "" {
		return Result{}, errors.New("ssh host requires address and user")
	}
	password, err := resolveSSHPassword(host)
	if err != nil {
		return Result{}, err
	}

	callback, err := e.hostKeyCallback()
	if err != nil {
		return Result{}, err
	}
	cfg := &ssh.ClientConfig{
		User:            host.User,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: callback,
		Timeout:         e.Timeout,
	}

	addr := host.Address
	if !strings.Contains(addr, ":") {
		port := host.Port
		if port == 0 {
			port = 22
		}
		addr = fmt.Sprintf("%s:%d", addr, port)
	}

	started := time.Now().UTC()
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return Result{}, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return Result{}, err
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return Result{}, err
	}

	var stdoutBuf strings.Builder
	var stderrBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go streamPipe(stdout, "stdout", sink, &stdoutBuf, &wg)
	go streamPipe(stderr, "stderr", sink, &stderrBuf, &wg)

	waitCh := make(chan error, 1)
	go func() { waitCh <- session.Run(command) }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		<-waitCh
		wg.Wait()
		return Result{}, ctx.Err()
	case waitErr := <-waitCh:
		wg.Wait()
		result := Result{
			Command:   command,
			Stdout:    stdoutBuf.String(),
			Stderr:    stderrBuf.String(),
			ExitCode:  exitCode(waitErr),
			StartedAt: started,
			EndedAt:   time.Now().UTC(),
		}
		result.Duration = result.EndedAt.Sub(started)
		return result, wrapWaitError(waitErr)
	}
}

func (e *Executor) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if strings.TrimSpace(e.KnownHostsPath) == "" {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	return knownhosts.New(e.KnownHostsPath)
}

func streamPipe(reader io.Reader, kind string, sink StreamSink, builder *strings.Builder, wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		builder.WriteString(line)
		builder.WriteByte('\n')
		if sink != nil {
			sink(kind, line+"\n")
		}
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	var sshErr *ssh.ExitError
	if errors.As(err, &sshErr) {
		return sshErr.ExitStatus()
	}
	return 1
}

func wrapWaitError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return err
	}
	return fmt.Errorf("command execution failed: %w", err)
}

func resolveSSHPassword(host models.Host) (string, error) {
	raw := strings.TrimSpace(host.PasswordEnv)
	if raw == "" {
		return "", errors.New("ssh host requires password or password_env")
	}
	if isEnvVarName(raw) {
		password := strings.TrimSpace(os.Getenv(raw))
		if password == "" {
			return "", fmt.Errorf("missing password from env %s", raw)
		}
		return password, nil
	}
	return raw, nil
}

func isEnvVarName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		switch {
		case r == '_':
			continue
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		default:
			return false
		}
	}
	return true
}
