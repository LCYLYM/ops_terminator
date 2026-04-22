package agent

import (
	"context"
	"testing"

	"osagentmvp/internal/approval"
	"osagentmvp/internal/builtin"
	"osagentmvp/internal/models"
	"osagentmvp/internal/policy"
	"osagentmvp/internal/runner"
)

type fakeClient struct {
	responses []*models.AssistantResponse
}

func (f *fakeClient) StreamChatCompletion(_ context.Context, _ []models.ChatMessage, _ []models.ToolDefinition, onText func(string)) (*models.AssistantResponse, error) {
	resp := f.responses[0]
	f.responses = f.responses[1:]
	if resp.Content != "" {
		onText(resp.Content)
	}
	return resp, nil
}

type fakeRecorder struct{}

func (fakeRecorder) RecordEvent(models.Event) error { return nil }

func TestRuntimeExecutesToolLoop(t *testing.T) {
	client := &fakeClient{
		responses: []*models.AssistantResponse{
			{ToolCalls: []models.ToolCall{{ID: "1", Type: "function", Function: models.ToolFunctionCall{Name: "hello_capability", Arguments: "{}"}}}},
			{Content: "done"},
		},
	}

	registry := builtin.NewRegistry(runner.NewExecutor(0, ""))
	runtime := New(client, registry, policy.New(), approval.NewManager(nil, fakeRecorder{}), fakeRecorder{})

	text, history, _, err := runtime.Execute(context.Background(), models.Run{ID: "run1"}, models.Host{Mode: models.HostModeLocal}, models.ContextSnapshot{}, "what can you do")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if text != "done" {
		t.Fatalf("unexpected final text: %q", text)
	}
	if len(history) == 0 {
		t.Fatal("expected tool history")
	}
}
