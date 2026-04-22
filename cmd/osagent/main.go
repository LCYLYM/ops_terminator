package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"osagentmvp/internal/agent"
	"osagentmvp/internal/approval"
	"osagentmvp/internal/builtin"
	"osagentmvp/internal/config"
	contextbuilder "osagentmvp/internal/context"
	"osagentmvp/internal/events"
	"osagentmvp/internal/gateway"
	"osagentmvp/internal/llm"
	"osagentmvp/internal/models"
	"osagentmvp/internal/policy"
	"osagentmvp/internal/runner"
	"osagentmvp/internal/skills"
	"osagentmvp/internal/store"
	"osagentmvp/internal/web"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "serve":
		must(runServe())
	case "hosts":
		must(runListHosts())
	case "host-add":
		must(runHostAdd())
	case "run":
		must(runCreateRun())
	case "runs":
		must(runListRuns())
	case "approvals":
		must(runListApprovals())
	case "approve":
		must(runResolveApproval())
	default:
		usage()
		os.Exit(1)
	}
}

func runServe() error {
	service, cfg, err := buildService()
	if err != nil {
		return err
	}
	if err := service.EnsureBootstrapState(); err != nil {
		return err
	}
	mux := http.NewServeMux()
	service.RegisterRoutes(mux)
	if err := web.Register(mux); err != nil {
		return err
	}
	server := &http.Server{
		Addr:              cfg.ServerAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Printf("OSAgent product MVP listening on http://127.0.0.1%s\n", cfg.ServerAddr)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	return server.ListenAndServe()
}

func runListHosts() error {
	var response struct {
		Items []models.Host `json:"items"`
	}
	if err := requestAPI(http.MethodGet, "/api/hosts", nil, &response); err != nil {
		return err
	}
	for _, host := range response.Items {
		fmt.Printf("%s\t%s\t%s\t%s\n", host.ID, host.DisplayName, host.Mode, host.Address)
	}
	return nil
}

func runHostAdd() error {
	host := models.Host{
		ID:          readFlag("--id"),
		DisplayName: readFlag("--name"),
		Mode:        readFlag("--mode"),
		Address:     readFlag("--address"),
		User:        readFlag("--user"),
		PasswordEnv: readFlag("--password-env"),
	}
	if host.ID == "" {
		return errors.New("--id is required")
	}
	if host.Mode == "" {
		host.Mode = models.HostModeLocal
	}
	if portRaw := readFlag("--port"); portRaw != "" {
		fmt.Sscanf(portRaw, "%d", &host.Port)
	}
	return requestAPI(http.MethodPost, "/api/hosts", host, nil)
}

func runCreateRun() error {
	hostID := readFlag("--host")
	input := readFlag("--input")
	if hostID == "" || input == "" {
		return errors.New("--host and --input are required")
	}
	var run models.Run
	if err := requestAPI(http.MethodPost, "/api/runs", gateway.RunRequest{
		HostID:      hostID,
		UserInput:   input,
		RequestedBy: "cli",
	}, &run); err != nil {
		return err
	}
	fmt.Printf("run created: %s\n", run.ID)
	return streamRun(run.ID)
}

func runListRuns() error {
	var response struct {
		Items []models.Run `json:"items"`
	}
	if err := requestAPI(http.MethodGet, "/api/runs", nil, &response); err != nil {
		return err
	}
	for _, run := range response.Items {
		fmt.Printf("%s\t%s\t%s\t%s\n", run.ID, run.HostID, run.Status, run.FinalResponse)
	}
	return nil
}

func runListApprovals() error {
	var response struct {
		Items []models.Approval `json:"items"`
	}
	if err := requestAPI(http.MethodGet, "/api/approvals", nil, &response); err != nil {
		return err
	}
	for _, approval := range response.Items {
		fmt.Printf("%s\trun=%s\ttool=%s\tdecision=%s\treason=%s\n", approval.ID, approval.RunID, approval.ToolName, approval.Decision, approval.Reason)
	}
	return nil
}

func runResolveApproval() error {
	approvalID := readFlag("--id")
	decision := readFlag("--decision")
	if approvalID == "" || decision == "" {
		return errors.New("--id and --decision are required")
	}
	return requestAPI(http.MethodPost, "/api/approvals/"+approvalID+"/resolve", map[string]any{
		"decision": decision,
		"actor":    "cli",
	}, nil)
}

func buildService() (*gateway.Service, config.Config, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return nil, config.Config{}, err
	}
	cfg, err := config.Load(workdir)
	if err != nil {
		return nil, config.Config{}, err
	}
	storeImpl, err := store.NewJSONStore(cfg.AbsDataDir())
	if err != nil {
		return nil, config.Config{}, err
	}
	hub := events.NewHub()
	logger := log.New(os.Stdout, "[osagent] ", log.LstdFlags|log.Lshortfile)
	executor := runner.NewExecutor(time.Duration(cfg.RunTimeoutSeconds)*time.Second, cfg.KnownHostsPath)
	registry := builtin.NewRegistry(executor)
	policyEngine := policy.New()
	skillCatalog, err := skills.Load(filepath.Join(workdir, "configs", "skills"))
	if err != nil {
		return nil, config.Config{}, err
	}
	builder := contextbuilder.NewBuilder(skillCatalog, registry, policyEngine)
	llmClient := llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model, time.Duration(cfg.RequestTimeoutSeconds)*time.Second)
	service := gateway.NewService(storeImpl, hub, builder, nil, logger)
	approvalManager := approval.NewManager(storeImpl, service)
	service.SetApprovals(approvalManager)
	runtime := agent.New(llmClient, registry, policyEngine, approvalManager, service)
	service.SetRuntime(runtime)
	return service, cfg, nil
}

func requestAPI(method, path string, body any, target any) error {
	workdir, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(workdir)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, "http://127.0.0.1"+cfg.ServerAddr+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api error: %s", strings.TrimSpace(string(raw)))
	}
	if target != nil {
		return json.NewDecoder(resp.Body).Decode(target)
	}
	return nil
}

func streamRun(runID string) error {
	workdir, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(workdir)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1"+cfg.ServerAddr+"/api/runs/"+runID+"/events/stream", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event models.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			continue
		}
		fmt.Printf("[%s] %s\n", event.Type, strings.TrimSpace(event.Message))
		if event.Type == "run.completed" || event.Type == "run.failed" {
			return nil
		}
	}
	return scanner.Err()
}

func readFlag(name string) string {
	for index := 2; index < len(os.Args); index++ {
		if os.Args[index] == name && index+1 < len(os.Args) {
			return os.Args[index+1]
		}
	}
	return ""
}

func usage() {
	fmt.Println(`usage:
  osagent serve
  osagent hosts
  osagent host-add --id local-demo --name Demo --mode local
  osagent run --host local --input "请检查磁盘空间"
  osagent runs
  osagent approvals
  osagent approve --id <approval-id> --decision approve`)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
