package builtin

import (
	"context"
	"strings"
	"testing"
	"time"

	"osagentmvp/internal/models"
	"osagentmvp/internal/runner"
)

func TestRegistryTruncatesModelResultButKeepsRawResult(t *testing.T) {
	registry := NewRegistry(runner.NewExecutor(5*time.Second, ""))
	settings := models.DefaultRuntimeSettings()
	settings.ToolResultMaxChars = 400
	settings.ToolResultHeadChars = 200
	settings.ToolResultTailChars = 80

	record, err := registry.Execute(context.Background(), models.Host{Mode: models.HostModeLocal}, models.ToolCall{
		ID:   "call_1",
		Type: "function",
		Function: models.ToolFunctionCall{
			Name:      "run_shell",
			Arguments: `{"command":"head -c 6000 /dev/zero | tr '\\\\0' 'a'"}`,
		},
	}, nil, settings)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !record.Truncated {
		t.Fatal("expected model result to be truncated")
	}
	if len(record.RawResult) <= len(record.ModelResult) {
		t.Fatalf("expected raw result to be longer than model result: raw=%d model=%d", len(record.RawResult), len(record.ModelResult))
	}
	if !strings.Contains(record.ModelResult, "truncated=true") {
		t.Fatalf("expected truncation marker in model result: %q", record.ModelResult)
	}
}
