package policy

import (
	"regexp"
	"strings"

	"osagentmvp/internal/models"
)

type Engine struct{}

func New() *Engine { return &Engine{} }

func (e *Engine) Summary() string {
	return "默认允许只读运维工具。涉及用户变更、服务重启和任意 shell 的操作进入审批。明显破坏性命令直接拒绝。"
}

func (e *Engine) Check(preview models.ActionPreview) models.PolicyRule {
	if preview.ToolName == "run_shell" {
		command := strings.ToLower(strings.TrimSpace(preview.CommandPreview))
		for _, pattern := range denyPatterns {
			if pattern.MatchString(command) {
				return models.PolicyRule{
					Decision:         models.PolicyDecisionDeny,
					Reason:           "命令触发了破坏性策略，已拒绝执行。",
					Scope:            preview.CommandPreview,
					SaferAlternative: "请先使用只读诊断工具缩小影响范围。",
				}
			}
		}
		return models.PolicyRule{
			Decision:         models.PolicyDecisionAsk,
			Reason:           "任意 shell 执行需要人工确认。",
			Scope:            preview.CommandPreview,
			SaferAlternative: "优先使用内置只读运维工具或更小范围命令。",
		}
	}

	if preview.ReadOnly {
		return models.PolicyRule{
			Decision: models.PolicyDecisionAllow,
			Reason:   "只读运维工具允许直接执行。",
			Scope:    preview.ToolName,
		}
	}

	return models.PolicyRule{
		Decision:         models.PolicyDecisionAsk,
		Reason:           firstNonEmpty(preview.RiskHint, "该操作可能修改系统状态，需要审批。"),
		Scope:            preview.ToolName,
		SaferAlternative: "请先执行只读检查，确认影响范围后再批准变更。",
	}
}

var denyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(^|[^a-z])(rm\s+-rf\s+/)([^a-z]|$)`),
	regexp.MustCompile(`\bmkfs\b`),
	regexp.MustCompile(`\bshutdown\b`),
	regexp.MustCompile(`\breboot\b`),
	regexp.MustCompile(`\bpoweroff\b`),
	regexp.MustCompile(`:\(\)\s*\{`),
	regexp.MustCompile(`\bdd\s+if=`),
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
