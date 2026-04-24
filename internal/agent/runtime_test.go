package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"osagentmvp/internal/approval"
	"osagentmvp/internal/builtin"
	"osagentmvp/internal/models"
	"osagentmvp/internal/policy"
	"osagentmvp/internal/runner"
)

type fakeClient struct {
	responses []*models.AssistantResponse
	calls     [][]models.ChatMessage
}

func (f *fakeClient) StreamChatCompletion(_ context.Context, messages []models.ChatMessage, _ []models.ToolDefinition, onText func(string)) (*models.AssistantResponse, error) {
	f.calls = append(f.calls, append([]models.ChatMessage(nil), messages...))
	resp := f.responses[0]
	f.responses = f.responses[1:]
	if resp.Content != "" {
		onText(resp.Content)
	}
	return resp, nil
}

type fakeRecorder struct{}

func (fakeRecorder) RecordEvent(models.Event) error { return nil }

type collectingRecorder struct {
	events []models.Event
}

func (r *collectingRecorder) RecordEvent(event models.Event) error {
	r.events = append(r.events, event)
	return nil
}

func TestRuntimeExecutesToolLoop(t *testing.T) {
	client := &fakeClient{
		responses: []*models.AssistantResponse{
			{ToolCalls: []models.ToolCall{{ID: "1", Type: "function", Function: models.ToolFunctionCall{Name: "hello_capability", Arguments: "{}"}}}},
			{Content: "done"},
		},
	}

	registry := builtin.NewRegistry(runner.NewExecutor(0, ""))
	runtime := New(client, registry, policy.New(), approval.NewManager(nil, fakeRecorder{}), fakeRecorder{})

	result, err := runtime.Execute(context.Background(), models.Run{ID: "run1"}, models.Host{Mode: models.HostModeLocal}, models.ConversationContext{
		Session: models.Session{},
		CurrentTurn: models.Turn{
			UserInput: "what can you do",
			ContextSnapshot: models.ContextSnapshot{
				PolicySummary: "policy",
			},
		},
		RuntimeSettings: models.DefaultRuntimeSettings(),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.FinalResponse != "done" {
		t.Fatalf("unexpected final text: %q", result.FinalResponse)
	}
	if len(result.ToolHistory) == 0 {
		t.Fatal("expected tool history")
	}
}

func TestRuntimeCarriesHistoricalMessagesForward(t *testing.T) {
	client := &fakeClient{
		responses: []*models.AssistantResponse{
			{Content: "follow-up done"},
		},
	}
	registry := builtin.NewRegistry(runner.NewExecutor(0, ""))
	runtime := New(client, registry, policy.New(), approval.NewManager(nil, fakeRecorder{}), fakeRecorder{})

	result, err := runtime.Execute(context.Background(), models.Run{ID: "run2"}, models.Host{ID: "local", DisplayName: "Local", Mode: models.HostModeLocal}, models.ConversationContext{
		Session: models.Session{
			Memory: models.MemoryState{
				HostProfile: models.HostProfile{Summary: "hostname: demo"},
			},
		},
		CurrentTurn: models.Turn{
			UserInput: "continue from the previous evidence",
			ContextSnapshot: models.ContextSnapshot{
				PolicySummary: "policy",
			},
		},
		HistoricalTurns: []models.Turn{
			{
				Messages: []models.ChatMessage{
					{Role: "user", Content: "check nginx 502"},
					{Role: "assistant", Content: "I found upstream timeouts"},
					{Role: "tool", Content: "command: grep upstream"},
				},
			},
		},
		RuntimeSettings: models.DefaultRuntimeSettings(),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.FinalResponse != "follow-up done" {
		t.Fatalf("unexpected final response: %q", result.FinalResponse)
	}
	if len(client.calls) == 0 {
		t.Fatal("expected chat client calls")
	}
	joined := ""
	for _, message := range client.calls[0] {
		joined += message.Content + "\n"
	}
	if !strings.Contains(joined, "I found upstream timeouts") {
		t.Fatalf("expected historical assistant content in prompt, got %q", joined)
	}
}

func TestRuntimeCompactsOldHistoryWhenThresholdExceeded(t *testing.T) {
	client := &fakeClient{
		responses: []*models.AssistantResponse{
			{Content: `{"user_goals":["stabilize nginx"],"confirmed_facts":["502 occurs on /api"],"tool_evidence":["grep upstream timeout"],"changes_made":["none"],"open_questions":["which upstream is failing"],"next_best_actions":["inspect upstream health"],"open_threads":["inspect upstream health"]}`},
			{Content: "compressed and continued"},
		},
	}
	registry := builtin.NewRegistry(runner.NewExecutor(0, ""))
	runtime := New(client, registry, policy.New(), approval.NewManager(nil, fakeRecorder{}), fakeRecorder{})

	longText := strings.Repeat("evidence ", 2000)
	settings := models.DefaultRuntimeSettings()
	settings.CompressionTriggerTokens = 100
	settings.ContextSoftLimitTokens = 120

	result, err := runtime.Execute(context.Background(), models.Run{ID: "run3"}, models.Host{ID: "local", DisplayName: "Local", Mode: models.HostModeLocal}, models.ConversationContext{
		Session: models.Session{
			Memory: models.MemoryState{
				HostProfile: models.HostProfile{Summary: "hostname: demo"},
			},
		},
		CurrentTurn: models.Turn{
			UserInput: "continue",
			ContextSnapshot: models.ContextSnapshot{
				PolicySummary: "policy",
			},
		},
		HistoricalTurns: []models.Turn{
			{ID: "turn_1", UserInput: "first", Messages: []models.ChatMessage{{Role: "user", Content: longText}, {Role: "assistant", Content: longText}}},
			{ID: "turn_2", UserInput: "second", Messages: []models.ChatMessage{{Role: "user", Content: longText}, {Role: "assistant", Content: longText}}},
			{ID: "turn_3", UserInput: "third", Messages: []models.ChatMessage{{Role: "user", Content: "recent"}, {Role: "assistant", Content: "recent answer"}}},
		},
		RuntimeSettings: settings,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.PromptStats.CompressionTriggered {
		t.Fatal("expected compression to trigger")
	}
	if !strings.Contains(result.Memory.RollingSummary, "confirmed_facts") {
		t.Fatalf("expected rolling summary to be populated, got %q", result.Memory.RollingSummary)
	}
	if result.Memory.CompressedUntilTurnID == "" {
		t.Fatal("expected compressed_until_turn_id to be set")
	}
}

func TestRuntimeUsesConfiguredMaxAgentSteps(t *testing.T) {
	client := &fakeClient{
		responses: []*models.AssistantResponse{
			{ToolCalls: []models.ToolCall{{ID: "1", Type: "function", Function: models.ToolFunctionCall{Name: "hello_capability", Arguments: "{}"}}}},
			{ToolCalls: []models.ToolCall{{ID: "2", Type: "function", Function: models.ToolFunctionCall{Name: "hello_capability", Arguments: "{}"}}}},
		},
	}
	registry := builtin.NewRegistry(runner.NewExecutor(0, ""))
	runtime := New(client, registry, policy.New(), approval.NewManager(nil, fakeRecorder{}), fakeRecorder{})

	settings := models.DefaultRuntimeSettings()
	settings.MaxAgentSteps = 2

	result, err := runtime.Execute(context.Background(), models.Run{ID: "run-max-steps"}, models.Host{Mode: models.HostModeLocal}, models.ConversationContext{
		Session: models.Session{},
		CurrentTurn: models.Turn{
			UserInput: "keep going until step limit",
			ContextSnapshot: models.ContextSnapshot{
				PolicySummary: "policy",
			},
		},
		RuntimeSettings: settings,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.FinalResponse != "agent stopped after reaching max step limit" {
		t.Fatalf("unexpected final response: %q", result.FinalResponse)
	}
	if len(client.calls) != 2 {
		t.Fatalf("expected exactly 2 model calls, got %d", len(client.calls))
	}
}

func TestRuntimeReturnsWholeToolBatchInSingleFollowupRequest(t *testing.T) {
	client := &fakeClient{
		responses: []*models.AssistantResponse{
			{ToolCalls: []models.ToolCall{
				{ID: "1", Type: "function", Function: models.ToolFunctionCall{Name: "hello_capability", Arguments: "{}"}},
				{ID: "2", Type: "function", Function: models.ToolFunctionCall{Name: "hello_capability", Arguments: "{}"}},
			}},
			{Content: "done"},
		},
	}
	recorder := &collectingRecorder{}
	registry := builtin.NewRegistry(runner.NewExecutor(0, ""))
	runtime := New(client, registry, policy.New(), approval.NewManager(nil, recorder), recorder)

	result, err := runtime.Execute(context.Background(), models.Run{ID: "run-parallel"}, models.Host{Mode: models.HostModeLocal}, models.ConversationContext{
		Session: models.Session{},
		CurrentTurn: models.Turn{
			UserInput: "call tools",
			ContextSnapshot: models.ContextSnapshot{
				PolicySummary: "policy",
			},
		},
		RuntimeSettings: models.DefaultRuntimeSettings(),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.FinalResponse != "done" {
		t.Fatalf("unexpected final response: %q", result.FinalResponse)
	}
	if len(result.ToolResults) != 2 {
		t.Fatalf("expected both tools to execute, got %d", len(result.ToolResults))
	}
	if len(client.calls) != 2 {
		t.Fatalf("expected one follow-up model request after the full tool batch, got %d calls", len(client.calls))
	}
	followup := client.calls[1]
	if len(followup) < 2 {
		t.Fatalf("expected batched tool messages in follow-up request, got %+v", followup)
	}
	last := followup[len(followup)-1]
	prev := followup[len(followup)-2]
	if prev.ToolCallID != "1" || last.ToolCallID != "2" {
		t.Fatalf("expected follow-up request to include both tool results in order, got %+v", followup)
	}
}

func TestRuntimeBypassApprovalsExecutesAskDecisionWithoutWaiting(t *testing.T) {
	outputPath := t.TempDir() + "/bypass.txt"
	client := &fakeClient{
		responses: []*models.AssistantResponse{
			{ToolCalls: []models.ToolCall{{ID: "1", Type: "function", Function: models.ToolFunctionCall{Name: "run_shell", Arguments: fmt.Sprintf(`{"command":"printf bypass > %s"}`, outputPath)}}}},
			{Content: "done"},
		},
	}
	recorder := &collectingRecorder{}
	registry := builtin.NewRegistry(runner.NewExecutor(0, ""))
	runtime := New(client, registry, policy.New(), approval.NewManager(nil, recorder), recorder)
	settings := models.DefaultRuntimeSettings()
	settings.BypassApprovals = true

	result, err := runtime.Execute(context.Background(), models.Run{ID: "run-bypass"}, models.Host{Mode: models.HostModeLocal}, models.ConversationContext{
		Session: models.Session{},
		CurrentTurn: models.Turn{
			UserInput: "run command",
			ContextSnapshot: models.ContextSnapshot{
				PolicySummary: "policy",
			},
		},
		RuntimeSettings: settings,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.FinalResponse != "done" {
		t.Fatalf("unexpected final response: %q", result.FinalResponse)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("expected bypassed tool execution, got %d results", len(result.ToolResults))
	}
	foundBypassEvent := false
	for _, event := range recorder.events {
		if event.Type == "run.approval_bypassed" {
			foundBypassEvent = true
		}
	}
	if !foundBypassEvent {
		t.Fatal("expected bypass event")
	}
}
