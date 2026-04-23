package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"osagentmvp/internal/approval"
	"osagentmvp/internal/builtin"
	"osagentmvp/internal/models"
	"osagentmvp/internal/policy"
)

type ChatClient interface {
	StreamChatCompletion(context.Context, []models.ChatMessage, []models.ToolDefinition, func(string)) (*models.AssistantResponse, error)
}

type EventRecorder interface {
	RecordEvent(models.Event) error
}

type Runtime struct {
	client    ChatClient
	registry  *builtin.Registry
	policy    *policy.Engine
	approvals *approval.Manager
	events    EventRecorder
}

const (
	maxToolHistoryEntryLength   = 320
	maxCommandPreviewEventChars = 240
	maxStreamEventBytesPerKind  = 8 * 1024
)

func New(client ChatClient, registry *builtin.Registry, policyEngine *policy.Engine, approvals *approval.Manager, events EventRecorder) *Runtime {
	return &Runtime{client: client, registry: registry, policy: policyEngine, approvals: approvals, events: events}
}

func (r *Runtime) Execute(ctx context.Context, run models.Run, host models.Host, convo models.ConversationContext) (models.ExecutionResult, error) {
	settings := normalizeRuntimeSettings(convo.RuntimeSettings)
	memory := convo.Session.Memory
	historyTurns := append([]models.Turn(nil), convo.HistoricalTurns...)
	toolDefs := r.registry.Definitions()

	beforeTokens, messages := r.buildConversationMessages(host, convo, memory, settings, historyTurns, toolDefs)
	compressedCount := 0
	compressionTriggered := false

	if beforeTokens >= settings.CompressionTriggerTokens {
		updatedMemory, count, changed, err := r.compactHistory(ctx, host, convo.Session, historyTurns, memory, settings)
		if err == nil && changed {
			memory = updatedMemory
			compressedCount = count
			compressionTriggered = true
			beforeTokens, messages = r.buildConversationMessages(host, convo, memory, settings, historyTurns, toolDefs)
		}
	}

	afterTokens, messages := r.buildConversationMessages(host, convo, memory, settings, historyTurns, toolDefs)
	currentTurnMessages := []models.ChatMessage{{Role: "user", Content: convo.CurrentTurn.UserInput}}
	var toolResults []models.ToolExecutionRecord
	var toolHistory []string
	var policyHistory []models.PolicyRule

	for step := 1; step <= settings.MaxAgentSteps; step++ {
		_ = r.RecordEvent(models.Event{
			ID:        models.NewID("event"),
			RunID:     run.ID,
			Type:      "run.running_agent",
			Message:   fmt.Sprintf("agent step %d", step),
			Timestamp: time.Now().UTC(),
		})

		var finalText strings.Builder
		response, err := r.client.StreamChatCompletion(ctx, messages, toolDefs, func(delta string) {
			finalText.WriteString(delta)
			_ = r.RecordEvent(models.Event{
				ID:        models.NewID("event"),
				RunID:     run.ID,
				Type:      "run.message_delta",
				Message:   delta,
				Timestamp: time.Now().UTC(),
			})
		})
		if err != nil {
			return models.ExecutionResult{}, err
		}

		assistantMessage := models.ChatMessage{
			Role:      "assistant",
			Content:   response.Content,
			ToolCalls: response.ToolCalls,
		}
		messages = append(messages, assistantMessage)
		currentTurnMessages = append(currentTurnMessages, assistantMessage)
		_ = r.RecordEvent(models.Event{
			ID:        models.NewID("event"),
			RunID:     run.ID,
			Type:      "run.assistant_message",
			Message:   firstNonEmpty(response.Content, finalText.String()),
			Payload:   map[string]any{"tool_calls": len(response.ToolCalls)},
			Timestamp: time.Now().UTC(),
		})

		if len(response.ToolCalls) == 0 {
			return models.ExecutionResult{
				FinalResponse: firstNonEmpty(response.Content, finalText.String()),
				ToolHistory:   toolHistory,
				PolicyHistory: policyHistory,
				Messages:      currentTurnMessages,
				ToolResults:   toolResults,
				PromptStats: models.PromptStats{
					EstimatedPromptTokensBeforeCompression: beforeTokens,
					EstimatedPromptTokens:                  afterTokens,
					CompressionTriggered:                   compressionTriggered,
					CompressedTurnCount:                    compressedCount,
					RecentFullTurnCount:                    settings.RecentFullTurns,
					MessageCount:                           len(messages),
				},
				Memory: memory,
			}, nil
		}

		for _, call := range response.ToolCalls {
			preview, err := r.registry.Preview(call)
			if err != nil {
				toolMessage := models.ChatMessage{Role: "tool", ToolCallID: call.ID, Content: "tool error: " + err.Error()}
				messages = append(messages, toolMessage)
				currentTurnMessages = append(currentTurnMessages, toolMessage)
				continue
			}
			rule := r.policy.Check(preview)
			policyHistory = append(policyHistory, rule)
			_ = r.RecordEvent(models.Event{
				ID:        models.NewID("event"),
				RunID:     run.ID,
				Type:      "run.policy_checked",
				Message:   rule.Reason,
				Payload:   map[string]any{"tool_name": preview.ToolName, "decision": rule.Decision, "scope": rule.Scope},
				Timestamp: time.Now().UTC(),
			})

			if rule.Decision == models.PolicyDecisionDeny {
				toolMessage := models.ChatMessage{Role: "tool", ToolCallID: call.ID, Content: "tool denied by policy: " + rule.Reason}
				messages = append(messages, toolMessage)
				currentTurnMessages = append(currentTurnMessages, toolMessage)
				toolHistory = append(toolHistory, preview.ToolName+": denied")
				continue
			}

			if rule.Decision == models.PolicyDecisionAsk {
				approvalRecord, approved, err := r.approvals.Wait(ctx, run.ID, preview, rule)
				if err != nil {
					return models.ExecutionResult{}, err
				}
				if !approved {
					toolMessage := models.ChatMessage{Role: "tool", ToolCallID: call.ID, Content: "tool denied by approval: " + approvalRecord.Reason}
					messages = append(messages, toolMessage)
					currentTurnMessages = append(currentTurnMessages, toolMessage)
					toolHistory = append(toolHistory, preview.ToolName+": approval denied")
					continue
				}
			}

			_ = r.RecordEvent(models.Event{
				ID:        models.NewID("event"),
				RunID:     run.ID,
				Type:      "run.tool_running",
				Message:   preview.ToolName,
				Payload:   map[string]any{"command_preview": truncateMiddle(preview.CommandPreview, maxCommandPreviewEventChars)},
				Timestamp: time.Now().UTC(),
			})

			streamSink := boundedStreamSink(maxStreamEventBytesPerKind, func(kind, chunk string) {
				eventType := "run.stdout"
				if kind == "stderr" {
					eventType = "run.stderr"
				}
				_ = r.RecordEvent(models.Event{
					ID:        models.NewID("event"),
					RunID:     run.ID,
					Type:      eventType,
					Message:   chunk,
					Timestamp: time.Now().UTC(),
				})
			})
			record, err := r.registry.Execute(ctx, host, call, streamSink, settings)
			record.ToolCallID = call.ID

			toolText := record.ModelResult
			if err != nil {
				toolText = strings.TrimSpace("tool error: " + err.Error() + "\n" + firstNonEmpty(record.ModelResult, record.RawResult))
			}
			toolMessage := models.ChatMessage{Role: "tool", ToolCallID: call.ID, Content: toolText}
			messages = append(messages, toolMessage)
			currentTurnMessages = append(currentTurnMessages, toolMessage)
			toolResults = append(toolResults, record)
			toolHistory = append(toolHistory, formatToolHistoryEntry(preview.ToolName, firstNonEmpty(record.ModelResult, record.RawResult)))
			_ = r.RecordEvent(models.Event{
				ID:        models.NewID("event"),
				RunID:     run.ID,
				Type:      "run.tool_finished",
				Message:   preview.ToolName,
				Payload:   map[string]any{"success": err == nil, "truncated": record.Truncated},
				Timestamp: time.Now().UTC(),
			})
		}
	}

	return models.ExecutionResult{
		FinalResponse: "agent stopped after reaching max step limit",
		ToolHistory:   toolHistory,
		PolicyHistory: policyHistory,
		Messages:      currentTurnMessages,
		ToolResults:   toolResults,
		PromptStats: models.PromptStats{
			EstimatedPromptTokensBeforeCompression: beforeTokens,
			EstimatedPromptTokens:                  afterTokens,
			CompressionTriggered:                   compressionTriggered,
			CompressedTurnCount:                    compressedCount,
			RecentFullTurnCount:                    settings.RecentFullTurns,
			MessageCount:                           len(messages),
		},
		Memory: memory,
	}, nil
}

func (r *Runtime) buildConversationMessages(host models.Host, convo models.ConversationContext, memory models.MemoryState, settings models.RuntimeSettings, historyTurns []models.Turn, toolDefs []models.ToolDefinition) (int, []models.ChatMessage) {
	messages := []models.ChatMessage{
		{Role: "system", Content: r.systemPrompt(convo.CurrentTurn.ContextSnapshot)},
		{Role: "system", Content: renderHostProfilePrompt(host, memory.HostProfile)},
	}
	if text := renderMemoryPrompt(memory); text != "" {
		messages = append(messages, models.ChatMessage{Role: "system", Content: text})
	}
	for _, turn := range selectRecentTurns(historyTurns, settings.RecentFullTurns) {
		messages = append(messages, filterConversationMessages(turn.Messages)...)
	}
	messages = append(messages, models.ChatMessage{Role: "user", Content: convo.CurrentTurn.UserInput})
	return estimateConversationTokens(messages, toolDefs), messages
}

func (r *Runtime) compactHistory(ctx context.Context, host models.Host, session models.Session, historyTurns []models.Turn, memory models.MemoryState, settings models.RuntimeSettings) (models.MemoryState, int, bool, error) {
	turnsToCompact := selectTurnsToCompact(historyTurns, memory.CompressedUntilTurnID, settings.RecentFullTurns)
	if len(turnsToCompact) == 0 {
		return memory, 0, false, nil
	}

	summary, openThreads, err := r.generateRollingSummary(ctx, host, memory, turnsToCompact)
	if err != nil {
		return memory, 0, false, err
	}

	now := time.Now().UTC()
	ledger := append([]string(nil), memory.OlderUserLedger...)
	for _, turn := range turnsToCompact {
		if strings.TrimSpace(turn.UserInput) != "" {
			ledger = append(ledger, turn.UserInput)
		}
	}
	if len(ledger) > settings.OlderUserLedgerEntries {
		ledger = ledger[len(ledger)-settings.OlderUserLedgerEntries:]
	}

	memory.RollingSummary = summary
	memory.OpenThreads = append([]string(nil), openThreads...)
	memory.OlderUserLedger = ledger
	memory.CompressedUntilTurnID = turnsToCompact[len(turnsToCompact)-1].ID
	memory.LastCompactedAt = &now
	return memory, len(turnsToCompact), true, nil
}

func (r *Runtime) generateRollingSummary(ctx context.Context, host models.Host, memory models.MemoryState, turns []models.Turn) (string, []string, error) {
	prompt := buildSummaryPrompt(host, memory, turns)
	response, err := r.client.StreamChatCompletion(ctx, []models.ChatMessage{
		{
			Role: "system",
			Content: strings.TrimSpace(`
You summarize ongoing agent work for future turns.
Return valid JSON only. Do not wrap in markdown fences.
Required keys:
- user_goals: string[]
- confirmed_facts: string[]
- tool_evidence: string[]
- changes_made: string[]
- open_questions: string[]
- next_best_actions: string[]
- open_threads: string[]
`),
		},
		{Role: "user", Content: prompt},
	}, nil, func(string) {})
	if err != nil {
		return "", nil, err
	}
	parsed, err := parseSummaryJSON(response.Content)
	if err != nil {
		return "", nil, err
	}
	return renderRollingSummary(parsed), parsed.OpenThreads, nil
}

func (r *Runtime) RecordEvent(event models.Event) error {
	if r.events != nil {
		return r.events.RecordEvent(event)
	}
	return nil
}

func (r *Runtime) systemPrompt(snapshot models.ContextSnapshot) string {
	var skills []string
	for _, skill := range snapshot.SkillSummaries {
		skills = append(skills, fmt.Sprintf("%s: %s", skill.ID, skill.Description))
	}
	primaryTool := "- run_shell: unavailable"
	var convenienceTools []string
	for _, tool := range snapshot.BuiltinSummaries {
		mode := "read-write"
		if tool.ReadOnly {
			mode = "read-only"
		}
		line := fmt.Sprintf("- %s (%s): %s", tool.Name, mode, tool.Description)
		if tool.Name == "run_shell" {
			primaryTool = line
			continue
		}
		convenienceTools = append(convenienceTools, line)
	}

	return strings.TrimSpace(fmt.Sprintf(`
You are an operations-system agent working on real Linux hosts.

Rules:
1. Prefer Linux commands and real command output over abstract planning.
2. Use tools to inspect first whenever practical.
3. Do not claim an action executed unless the tool result shows it.
4. Prefer read-only commands before mutating commands.
5. For Linux-specific investigation or change operations, prefer run_shell unless a builtin clearly reduces ambiguity or enforces a safer parameter shape.
6. Keep run_shell commands explicit and minimal. Avoid shell tricks, nested interpreters, background jobs, command substitution, and broad destructive patterns.
7. Use specialized builtin tools as convenience shortcuts for common diagnostics or approved mutating actions, not as your default first choice.
8. Command results may be truncated; if evidence is insufficient, run a narrower follow-up command instead of guessing.
9. If enough evidence is collected, stop calling tools and answer directly.
10. The control plane will enforce approval and safety. You should still minimize risk.

Session summary:
%s

Relevant skills:
%s

Policy summary:
%s

Primary execution tool:
%s

Convenience builtins:
%s
`, snapshot.SessionSummary, strings.Join(skills, "\n"), snapshot.PolicySummary, primaryTool, strings.Join(convenienceTools, "\n")))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func formatToolHistoryEntry(toolName, text string) string {
	return toolName + ": " + truncateMiddle(strings.TrimSpace(text), maxToolHistoryEntryLength)
}

func truncateMiddle(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 24 {
		return value[:limit]
	}
	notice := fmt.Sprintf(" ... [truncated %d chars] ... ", len(value)-limit)
	head := (limit - len(notice)) / 2
	tail := limit - len(notice) - head
	if head < 1 {
		head = 1
	}
	if tail < 1 {
		tail = 1
	}
	return value[:head] + notice + value[len(value)-tail:]
}

func boundedStreamSink(limit int, sink func(kind, chunk string)) func(kind, chunk string) {
	type streamState struct {
		used      int
		truncated bool
	}
	states := map[string]*streamState{}
	return func(kind, chunk string) {
		if sink == nil || chunk == "" {
			return
		}
		state, ok := states[kind]
		if !ok {
			state = &streamState{}
			states[kind] = state
		}
		if state.truncated {
			return
		}
		remaining := limit - state.used
		if remaining <= 0 {
			sink(kind, fmt.Sprintf("[truncated further %s output after %d bytes]\n", kind, limit))
			state.truncated = true
			return
		}
		if len(chunk) <= remaining {
			sink(kind, chunk)
			state.used += len(chunk)
			return
		}
		sink(kind, chunk[:remaining])
		sink(kind, fmt.Sprintf("\n[truncated further %s output after %d bytes]\n", kind, limit))
		state.used = limit
		state.truncated = true
	}
}
