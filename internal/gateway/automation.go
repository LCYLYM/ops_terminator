package gateway

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"osagentmvp/internal/models"
)

func (s *Service) StartAutomationLoop(ctx context.Context) {
	if s.executor == nil {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			if err := s.runAutomationCycle(ctx); err != nil && s.logger != nil {
				s.logger.Printf("automation cycle failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Service) runAutomationCycle(ctx context.Context) error {
	rules, err := s.store.ListAutomations()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		host, found, err := s.store.GetHost(rule.HostID)
		if err != nil || !found {
			continue
		}
		value, err := s.sampleAutomationMetric(ctx, host, rule.Metric)
		rule.LastObservedValue = value
		if err != nil {
			rule.LastStatus = "sample_failed"
			rule.UpdatedAt = now
			_ = s.store.SaveAutomation(rule)
			continue
		}
		if !automationThresholdMatched(value, rule.Operator, rule.Threshold) {
			rule.LastStatus = "healthy"
			rule.UpdatedAt = now
			_ = s.store.SaveAutomation(rule)
			continue
		}
		if isAutomationCooldownBlocked(rule, now) {
			rule.LastStatus = "cooldown"
			rule.UpdatedAt = now
			_ = s.store.SaveAutomation(rule)
			continue
		}

		run, err := s.createAutomationRun(ctx, rule, value, "automation")
		if err != nil {
			rule.LastStatus = "trigger_failed"
			rule.UpdatedAt = now
			_ = s.store.SaveAutomation(rule)
			continue
		}
		rule.LastTriggeredAt = &now
		rule.LastRunID = run.ID
		rule.LastStatus = run.Status
		rule.SessionID = run.SessionID
		rule.UpdatedAt = now
		_ = s.store.SaveAutomation(rule)
	}
	return nil
}

func (s *Service) createAutomationRun(ctx context.Context, rule models.AutomationRule, value float64, requestedBy string) (models.Run, error) {
	bypass := rule.BypassApprovals
	return s.CreateRun(ctx, RunRequest{
		HostID:          rule.HostID,
		SessionID:       rule.SessionID,
		UserInput:       buildAutomationPrompt(rule, value, requestedBy),
		RequestedBy:     requestedBy,
		BypassApprovals: &bypass,
	})
}

func (s *Service) sampleAutomationMetric(ctx context.Context, host models.Host, metric string) (float64, error) {
	command, err := automationMetricCommand(metric)
	if err != nil {
		return 0, err
	}
	result, err := s.executor.Run(ctx, host, command, nil)
	if err != nil {
		return 0, err
	}
	return parseMetricValue(firstNonEmpty(result.Stdout, result.Stderr))
}

func automationThresholdMatched(value float64, operator string, threshold float64) bool {
	switch strings.TrimSpace(operator) {
	case ">", "gt":
		return value > threshold
	case ">=", "gte":
		return value >= threshold
	case "<", "lt":
		return value < threshold
	case "<=", "lte":
		return value <= threshold
	default:
		return false
	}
}

func isSupportedAutomationOperator(operator string) bool {
	switch strings.TrimSpace(operator) {
	case ">", "gt", ">=", "gte", "<", "lt", "<=", "lte":
		return true
	default:
		return false
	}
}

func isAutomationCooldownBlocked(rule models.AutomationRule, now time.Time) bool {
	return rule.LastTriggeredAt != nil && now.Sub(*rule.LastTriggeredAt) < time.Duration(rule.CooldownMinutes)*time.Minute
}

func buildAutomationPrompt(rule models.AutomationRule, value float64, requestedBy string) string {
	base := strings.TrimSpace(rule.PromptTemplate)
	if base == "" {
		base = "阈值触发，请检查并处理当前异常。"
	}
	return fmt.Sprintf("%s\n\n触发信息:\n- automation_id: %s\n- automation_name: %s\n- requested_by: %s\n- metric: %s\n- operator: %s\n- threshold: %.2f\n- observed_value: %.2f\n- trigger_type: %s\n\n请明确这是自动化触发，不是人工即时输入；先诊断再给出可审计的处理步骤。", base, rule.ID, rule.Name, requestedBy, rule.Metric, rule.Operator, rule.Threshold, value, rule.TriggerType)
}

func parseMetricValue(output string) (float64, error) {
	text := strings.TrimSpace(output)
	if text == "" {
		return 0, fmt.Errorf("empty metric output")
	}
	return strconv.ParseFloat(text, 64)
}

func automationMetricCommand(metric string) (string, error) {
	switch strings.TrimSpace(metric) {
	case "cpu_usage":
		return "(top -bn1 2>/dev/null | awk '/Cpu\\(s\\)|^%Cpu/ {for (i=1;i<=NF;i++) if ($i ~ /id,|id$/) {gsub(/,/, \"\", $(i-1)); print 100-$(i-1); exit}}') || (top -l 1 -n 0 2>/dev/null | awk -F'[:,%]' '/CPU usage/ {print $2+$4; exit}')", nil
	case "memory_usage":
		return "(free 2>/dev/null | awk '/Mem:/ {print ($3/$2)*100; exit}') || (memory_pressure 2>/dev/null | awk -F': ' '/System-wide memory free percentage/ {gsub(/%/, \"\", $2); print 100-$2; exit}')", nil
	case "disk_usage":
		return "df -Pk / | awk 'NR==2 {gsub(/%/, \"\", $5); print $5; exit}'", nil
	case "inode_usage":
		return "df -Pi / 2>/dev/null | awk 'NR==2 {gsub(/%/, \"\", $5); print $5; exit}'", nil
	default:
		return "", fmt.Errorf("unsupported automation metric: %s", metric)
	}
}
