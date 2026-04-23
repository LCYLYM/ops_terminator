package runner

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"osagentmvp/internal/models"
)

func TestRunLocal(t *testing.T) {
	executor := NewExecutor(5*time.Second, "")
	result, err := executor.Run(context.Background(), models.Host{Mode: models.HostModeLocal}, "printf 'hello\\n'", nil)
	if err != nil {
		t.Fatalf("run local: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Fatalf("unexpected stdout: %q", result.Stdout)
	}
}

func TestResolveSSHPasswordFromLiteral(t *testing.T) {
	password, err := resolveSSHPassword(models.Host{PasswordEnv: "plain-secret"})
	if err != nil {
		t.Fatalf("resolve literal password: %v", err)
	}
	if password != "plain-secret" {
		t.Fatalf("unexpected password: %q", password)
	}
}

func TestResolveSSHPasswordFromEnv(t *testing.T) {
	t.Setenv("OSAGENT_SSH_PASSWORD_DEMO", "from-env")
	password, err := resolveSSHPassword(models.Host{PasswordEnv: "OSAGENT_SSH_PASSWORD_DEMO"})
	if err != nil {
		t.Fatalf("resolve env password: %v", err)
	}
	if password != "from-env" {
		t.Fatalf("unexpected password: %q", password)
	}
}

func TestResolveSSHPasswordMissingEnv(t *testing.T) {
	os.Unsetenv("OSAGENT_SSH_PASSWORD_MISSING")
	_, err := resolveSSHPassword(models.Host{PasswordEnv: "OSAGENT_SSH_PASSWORD_MISSING"})
	if err == nil {
		t.Fatal("expected missing env error")
	}
	if !strings.Contains(err.Error(), "missing password from env") {
		t.Fatalf("unexpected error: %v", err)
	}
}
