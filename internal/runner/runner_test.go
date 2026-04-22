package runner

import (
	"context"
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
