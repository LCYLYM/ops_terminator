package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"osagentmvp/internal/models"
)

type summaryPayload struct {
	UserGoals       []string `json:"user_goals"`
	ConfirmedFacts  []string `json:"confirmed_facts"`
	ToolEvidence    []string `json:"tool_evidence"`
	ChangesMade     []string `json:"changes_made"`
	OpenQuestions   []string `json:"open_questions"`
	NextBestActions []string `json:"next_best_actions"`
	OpenThreads     []string `json:"open_threads"`
}

func normalizeRuntimeSettings(settings models.RuntimeSettings) models.RuntimeSettings {
	defaults := models.DefaultRuntimeSettings()
	if settings.MaxAgentSteps <= 0 {
		settings.MaxAgentSteps = defaults.MaxAgentSteps
	}
	if settings.ContextSoftLimitTokens <= 0 {
		settings.ContextSoftLimitTokens = defaults.ContextSoftLimitTokens
	}
	if settings.CompressionTriggerTokens <= 0 {
		settings.CompressionTriggerTokens = defaults.CompressionTriggerTokens
	}
	if settings.ResponseReserveTokens <= 0 {
		settings.ResponseReserveTokens = defaults.ResponseReserveTokens
	}
	if settings.RecentFullTurns <= 0 {
		settings.RecentFullTurns = defaults.RecentFullTurns
	}
	if settings.OlderUserLedgerEntries <= 0 {
		settings.OlderUserLedgerEntries = defaults.OlderUserLedgerEntries
	}
	if settings.HostProfileTTLMinutes <= 0 {
		settings.HostProfileTTLMinutes = defaults.HostProfileTTLMinutes
	}
	if settings.ToolResultMaxChars <= 0 {
		settings.ToolResultMaxChars = defaults.ToolResultMaxChars
	}
	if settings.ToolResultHeadChars <= 0 {
		settings.ToolResultHeadChars = defaults.ToolResultHeadChars
	}
	if settings.ToolResultTailChars < 0 {
		settings.ToolResultTailChars = defaults.ToolResultTailChars
	}
	return settings
}

func selectRecentTurns(turns []models.Turn, recentFullTurns int) []models.Turn {
	if recentFullTurns <= 0 || len(turns) <= recentFullTurns {
		return append([]models.Turn(nil), turns...)
	}
	return append([]models.Turn(nil), turns[len(turns)-recentFullTurns:]...)
}

func selectTurnsToCompact(turns []models.Turn, compressedUntilTurnID string, recentFullTurns int) []models.Turn {
	keepStart := len(turns) - recentFullTurns
	if keepStart <= 0 {
		return nil
	}
	start := 0
	if compressedUntilTurnID != "" {
		for i, turn := range turns {
			if turn.ID == compressedUntilTurnID {
				start = i + 1
				break
			}
		}
	}
	if start >= keepStart {
		return nil
	}
	return append([]models.Turn(nil), turns[start:keepStart]...)
}

func filterConversationMessages(items []models.ChatMessage) []models.ChatMessage {
	result := make([]models.ChatMessage, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Role) == "" {
			continue
		}
		result = append(result, item)
	}
	return result
}

func estimateConversationTokens(messages []models.ChatMessage, tools []models.ToolDefinition) int {
	totalChars := 0
	for _, message := range messages {
		totalChars += len(message.Role) + len(message.Content) + 12
		for _, call := range message.ToolCalls {
			totalChars += len(call.ID) + len(call.Type) + len(call.Function.Name) + len(call.Function.Arguments)
		}
	}
	for _, tool := range tools {
		totalChars += len(tool.Type) + len(tool.Function.Name) + len(tool.Function.Description)
	}
	base := int(math.Ceil(float64(totalChars) / 3.0))
	return int(math.Ceil(float64(base) * 1.2))
}

func renderHostProfilePrompt(host models.Host, profile models.HostProfile) string {
	summary := strings.TrimSpace(profile.Summary)
	if summary == "" {
		summary = "host profile unavailable"
	}
	return strings.TrimSpace(fmt.Sprintf(`
Current host:
- id: %s
- name: %s
- mode: %s

Host environment:
%s
`, host.ID, host.DisplayName, host.Mode, summary))
}

func renderMemoryPrompt(memory models.MemoryState) string {
	var sections []string
	if strings.TrimSpace(memory.RollingSummary) != "" {
		sections = append(sections, "Rolling memory:\n"+memory.RollingSummary)
	}
	if len(memory.OlderUserLedger) > 0 {
		sections = append(sections, "Older user ledger:\n- "+strings.Join(memory.OlderUserLedger, "\n- "))
	}
	if len(memory.OpenThreads) > 0 {
		sections = append(sections, "Open threads:\n- "+strings.Join(memory.OpenThreads, "\n- "))
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func buildSummaryPrompt(host models.Host, memory models.MemoryState, turns []models.Turn) string {
	var builder strings.Builder
	builder.WriteString("Existing rolling summary:\n")
	builder.WriteString(firstNonEmpty(memory.RollingSummary, "(none)"))
	builder.WriteString("\n\nHost profile:\n")
	builder.WriteString(firstNonEmpty(memory.HostProfile.Summary, host.DisplayName))
	builder.WriteString("\n\nTurns to compact:\n")
	for idx, turn := range turns {
		builder.WriteString(fmt.Sprintf("Turn %d user: %s\n", idx+1, turn.UserInput))
		for _, message := range turn.Messages {
			if strings.TrimSpace(message.Role) == "" {
				continue
			}
			builder.WriteString(fmt.Sprintf("%s: %s\n", message.Role, strings.TrimSpace(message.Content)))
		}
		for _, record := range turn.ToolResults {
			builder.WriteString(fmt.Sprintf("tool[%s]: %s\n", record.ToolName, strings.TrimSpace(firstNonEmpty(record.ModelResult, record.RawResult))))
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func parseSummaryJSON(value string) (summaryPayload, error) {
	var payload summaryPayload
	raw := strings.TrimSpace(value)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end >= start {
		raw = raw[start : end+1]
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return summaryPayload{}, err
	}
	return payload, nil
}

func renderRollingSummary(payload summaryPayload) string {
	sections := []struct {
		title string
		items []string
	}{
		{title: "user_goals", items: payload.UserGoals},
		{title: "confirmed_facts", items: payload.ConfirmedFacts},
		{title: "tool_evidence", items: payload.ToolEvidence},
		{title: "changes_made", items: payload.ChangesMade},
		{title: "open_questions", items: payload.OpenQuestions},
		{title: "next_best_actions", items: payload.NextBestActions},
	}
	var parts []string
	for _, section := range sections {
		if len(section.items) == 0 {
			parts = append(parts, section.title+": none")
			continue
		}
		parts = append(parts, section.title+":\n- "+strings.Join(section.items, "\n- "))
	}
	return strings.Join(parts, "\n\n")
}
