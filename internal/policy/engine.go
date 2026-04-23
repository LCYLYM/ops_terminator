package policy

import (
	"regexp"
	"strings"

	"osagentmvp/internal/models"
)

type Engine struct{}

func New() *Engine { return &Engine{} }

func (e *Engine) Summary() string {
	return "默认允许只读运维工具。run_shell 会先按命令内容判断：只读诊断命令直通、变更命令走审批、明显破坏性命令直接拒绝。"
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
		for _, pattern := range allowReadOnlyShellPatterns {
			if pattern.MatchString(command) {
				return models.PolicyRule{
					Decision: models.PolicyDecisionAllow,
					Reason:   "命令匹配只读诊断规则，允许直接执行。",
					Scope:    preview.CommandPreview,
				}
			}
		}
		return models.PolicyRule{
			Decision:         models.PolicyDecisionAsk,
			Reason:           "命令可能修改系统状态，需人工确认后执行。",
			Scope:            preview.CommandPreview,
			SaferAlternative: "优先使用只读命令先确认环境，再审批变更动作。",
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

var allowReadOnlyShellPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*(hostname|uname|whoami|pwd|date)(\s|$)`),
	regexp.MustCompile(`^\s*(df|du|free|vm_stat|swapon|lsblk|mount)(\s|$)`),
	regexp.MustCompile(`^\s*(ps|pgrep|top|vmstat|iostat|sar)(\s|$)`),
	regexp.MustCompile(`^\s*(ss|netstat|lsof)(\s|$)`),
	regexp.MustCompile(`^\s*(cat|less|more|tail|head|grep|egrep|awk|sed\s+-n)(\s|$)`),
	regexp.MustCompile(`^\s*(find|ls|stat|file|readlink|realpath)(\s|$)`),
	regexp.MustCompile(`^\s*(id|getent|groups)(\s|$)`),
	regexp.MustCompile(`^\s*(systemctl\s+status|service\s+\S+\s+status|journalctl)(\s|$)`),
	regexp.MustCompile(`^\s*(apt-cache|apt\s+list|dnf\s+list|yum\s+list|rpm\s+-q|dpkg\s+-s|zypper\s+search|pacman\s+-Q)(\s|$)`),
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
