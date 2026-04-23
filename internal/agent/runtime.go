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

func New(client ChatClient, registry *builtin.Registry, policyEngine *policy.Engine, approvals *approval.Manager, events EventRecorder) *Runtime {
	return &Runtime{client: client, registry: registry, policy: policyEngine, approvals: approvals, events: events}
}

func (r *Runtime) Execute(ctx context.Context, run models.Run, host models.Host, snapshot models.ContextSnapshot, userInput string) (string, []string, []models.PolicyRule, error) {
	messages := []models.ChatMessage{
		{Role: "system", Content: r.systemPrompt(snapshot)},
		{Role: "user", Content: userInput},
	}

	var toolHistory []string
	var policyHistory []models.PolicyRule

	for step := 1; step <= 8; step++ {
		_ = r.RecordEvent(models.Event{
			ID:        models.NewID("event"),
			RunID:     run.ID,
			Type:      "run.running_agent",
			Message:   fmt.Sprintf("agent step %d", step),
			Timestamp: time.Now().UTC(),
		})

		var finalText strings.Builder
		response, err := r.client.StreamChatCompletion(ctx, messages, r.registry.Definitions(), func(delta string) {
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
			return "", toolHistory, policyHistory, err
		}

		assistantMessage := models.ChatMessage{
			Role:      "assistant",
			Content:   response.Content,
			ToolCalls: response.ToolCalls,
		}
		messages = append(messages, assistantMessage)
		_ = r.RecordEvent(models.Event{
			ID:        models.NewID("event"),
			RunID:     run.ID,
			Type:      "run.assistant_message",
			Message:   firstNonEmpty(response.Content, finalText.String()),
			Payload:   map[string]any{"tool_calls": len(response.ToolCalls)},
			Timestamp: time.Now().UTC(),
		})

		if len(response.ToolCalls) == 0 {
			return firstNonEmpty(response.Content, finalText.String()), toolHistory, policyHistory, nil
		}

		for _, call := range response.ToolCalls {
			preview, err := r.registry.Preview(call)
			if err != nil {
				toolMessage := models.ChatMessage{Role: "tool", ToolCallID: call.ID, Content: "tool error: " + err.Error()}
				messages = append(messages, toolMessage)
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
				toolHistory = append(toolHistory, preview.ToolName+": denied")
				continue
			}

			if rule.Decision == models.PolicyDecisionAsk {
				approvalRecord, approved, err := r.approvals.Wait(ctx, run.ID, preview, rule)
				if err != nil {
					return "", toolHistory, policyHistory, err
				}
				if !approved {
					toolMessage := models.ChatMessage{Role: "tool", ToolCallID: call.ID, Content: "tool denied by approval: " + approvalRecord.Reason}
					messages = append(messages, toolMessage)
					toolHistory = append(toolHistory, preview.ToolName+": approval denied")
					continue
				}
			}

			_ = r.RecordEvent(models.Event{
				ID:        models.NewID("event"),
				RunID:     run.ID,
				Type:      "run.tool_running",
				Message:   preview.ToolName,
				Payload:   map[string]any{"command_preview": preview.CommandPreview},
				Timestamp: time.Now().UTC(),
			})
			text, err := r.registry.Execute(ctx, host, call, func(kind, chunk string) {
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
			toolHistory = append(toolHistory, preview.ToolName+": "+strings.TrimSpace(text))
			if err != nil {
				text = "tool error: " + err.Error() + "\n" + text
			}
			messages = append(messages, models.ChatMessage{Role: "tool", ToolCallID: call.ID, Content: text})
			_ = r.RecordEvent(models.Event{
				ID:        models.NewID("event"),
				RunID:     run.ID,
				Type:      "run.tool_finished",
				Message:   preview.ToolName,
				Payload:   map[string]any{"success": err == nil},
				Timestamp: time.Now().UTC(),
			})
		}
	}

	return "agent stopped after reaching max step limit", toolHistory, policyHistory, nil
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
	var tools []string
	for _, tool := range snapshot.BuiltinSummaries {
		mode := "read-write"
		if tool.ReadOnly {
			mode = "read-only"
		}
		tools = append(tools, fmt.Sprintf("%s (%s): %s", tool.Name, mode, tool.Description))
	}

	return strings.TrimSpace(fmt.Sprintf(`
You are an operations-system agent working on real Linux hosts.

Rules:
1. Prefer Linux commands and real command output over abstract planning.
2. Use tools to inspect first whenever practical.
3. Do not claim an action executed unless the tool result shows it.
4. Prefer read-only commands before mutating commands.
5. For Linux-specific investigation or change operations, run_shell is acceptable and often preferred.
6. Use specialized builtin tools only when they clearly reduce ambiguity or improve portability.
7. If enough evidence is collected, stop calling tools and answer directly.
8. The control plane will enforce approval and safety. You should still minimize risk.

Current host:
- id: %s
- name: %s
- mode: %s

Session summary:
%s

Relevant skills:
%s

Policy summary:
%s

Builtin tools:
%s
`, snapshot.HostID, snapshot.HostDisplayName, snapshot.HostMode, snapshot.SessionSummary, strings.Join(skills, "\n"), snapshot.PolicySummary, strings.Join(tools, "\n")))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
