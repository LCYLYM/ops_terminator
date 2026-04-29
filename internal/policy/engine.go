package policy

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"osagentmvp/internal/models"
)

type Engine struct {
	mu    sync.RWMutex
	rules map[string]models.PolicyRuleConfig
}

func New(configs ...[]models.PolicyRuleConfig) *Engine {
	engine := &Engine{}
	rules := DefaultRuleConfigs()
	if len(configs) > 0 && len(configs[0]) > 0 {
		rules = configs[0]
	}
	_ = engine.UpdateRules(rules)
	return engine
}

func DefaultRuleConfigs() []models.PolicyRuleConfig {
	return []models.PolicyRuleConfig{
		ruleConfig("readonly_builtin_allow", "builtin", "low", models.PolicyDecisionAllow, "只读运维工具允许直接执行。", "", false, "Allow explicit read-only builtin tools."),
		ruleConfig("mutating_builtin_ask", "builtin", "high", models.PolicyDecisionAsk, "该操作可能修改系统状态，需要审批。", "请先执行只读检查，确认影响范围后再批准变更。", true, "Require human approval for builtin tools that can change host state."),
		ruleConfig("readonly_shell_allow", "shell", "low", models.PolicyDecisionAllow, "命令经安全解析后仅包含显式只读诊断步骤，允许直接执行。", "", false, "Allow parsed shell commands that only contain explicit read-only diagnostics."),
		ruleConfig("mutating_shell_ask", "shell", "high", models.PolicyDecisionAsk, "命令包含可能修改系统状态的步骤，需人工确认后执行。", "优先拆成更小的只读 run_shell 诊断；若确需变更，请走审批执行。", true, "Require approval for shell commands that may modify files, services, users, packages, or system state."),
		ruleConfig("destructive_command_deny", "shell", "critical", models.PolicyDecisionDeny, "命令包含高危或破坏性操作，已拒绝执行。", "请先执行只读诊断命令缩小影响范围，再用审批内置动作或更小范围命令处理。", false, "Reject destructive commands such as filesystem formatting, shutdown, reboot, raw block writes, or broad root deletion."),
		ruleConfig("remote_download_pipe_shell_deny", "shell", "critical", models.PolicyDecisionDeny, "检测到远程下载内容直接通过管道交给 shell，已拒绝执行。", "请先单独下载并检查内容，再执行明确命令。", false, "Reject remote download output piped directly into a shell interpreter."),
		ruleConfig("nested_interpreter_deny", "shell", "high", models.PolicyDecisionDeny, "命令尝试通过解释器再次嵌套执行，已拒绝执行。", "请直接把要执行的命令展开成显式 run_shell，而不是再包一层解释器。", false, "Reject nested shell or scripting interpreter execution through flags such as -c and -e."),
		ruleConfig("complex_shell_syntax_deny", "shell", "high", models.PolicyDecisionDeny, "命令包含未支持或不安全的 shell 语法，已拒绝执行。", "请改成显式、扁平的命令，不要使用命令替换、后台执行、here-doc 或嵌套解释器。", false, "Reject unsupported shell constructs such as command substitution, here-docs, background execution, or process substitution."),
		ruleConfig("empty_shell_deny", "shell", "medium", models.PolicyDecisionDeny, "空命令已拒绝执行。", "请提供明确的 Linux 命令。", false, "Reject empty shell commands."),
		ruleConfig("shell_parse_error_deny", "shell", "medium", models.PolicyDecisionDeny, "命令解析失败，已拒绝执行。", "请改用更直接的命令表达，避免复杂转义。", false, "Reject shell commands that cannot be parsed safely."),
		ruleConfig("missing_executable_deny", "shell", "medium", models.PolicyDecisionDeny, "命令缺少可执行程序，已拒绝执行。", "请提供显式命令，不要只传环境变量或残缺片段。", false, "Reject shell snippets without an executable command."),
	}
}

func ruleConfig(id, category, severity, decision, reason, saferAlternative string, overrideAllowed bool, description string) models.PolicyRuleConfig {
	return models.PolicyRuleConfig{
		ID:               id,
		Category:         category,
		Severity:         severity,
		Decision:         decision,
		Reason:           reason,
		SaferAlternative: saferAlternative,
		OverrideAllowed:  overrideAllowed,
		Description:      description,
		UpdatedAt:        time.Now().UTC(),
	}
}

func (e *Engine) RuleConfigs() []models.PolicyRuleConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	items := make([]models.PolicyRuleConfig, 0, len(e.rules))
	for _, item := range e.rules {
		items = append(items, item)
	}
	return sortRuleConfigs(items)
}

func (e *Engine) UpdateRules(rules []models.PolicyRuleConfig) error {
	normalized, err := normalizeRuleConfigs(rules)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = make(map[string]models.PolicyRuleConfig, len(normalized))
	for _, item := range normalized {
		e.rules[item.ID] = item
	}
	return nil
}

func (e *Engine) Summary() string {
	return "默认允许只读运维工具。run_shell 会先做安全解析：显式只读命令直接放行，可能改写系统状态的命令进入审批，复杂绕过语法或明显破坏性命令直接拒绝。"
}

func (e *Engine) Check(preview models.ActionPreview) models.PolicyRule {
	if preview.ToolName == "run_shell" {
		return e.checkRunShell(preview)
	}

	if preview.ReadOnly {
		return e.newPolicyRule("readonly_builtin_allow", "builtin", "low", models.PolicyDecisionAllow, "只读运维工具允许直接执行。", preview.ToolName, "", false)
	}

	return e.newPolicyRule("mutating_builtin_ask", "builtin", "high", models.PolicyDecisionAsk, firstNonEmpty(preview.RiskHint, "该操作可能修改系统状态，需要审批。"), preview.ToolName, "请先执行只读检查，确认影响范围后再批准变更。", true)
}

func (e *Engine) checkRunShell(preview models.ActionPreview) models.PolicyRule {
	command := strings.TrimSpace(preview.CommandPreview)
	if command == "" {
		return e.newPolicyRule("empty_shell_deny", "shell", "medium", models.PolicyDecisionDeny, "空命令已拒绝执行。", preview.ToolName, "请提供明确的 Linux 命令。", false)
	}

	assessment := assessShellCommand(command)
	return e.newPolicyRule(assessment.RuleID, assessment.Category, assessment.Severity, assessment.Decision, assessment.Reason, command, assessment.SaferAlternative, assessment.OverrideAllowed)
}

type shellAssessment struct {
	Decision         string
	RuleID           string
	Category         string
	Severity         string
	Reason           string
	SaferAlternative string
	OverrideAllowed  bool
}

type shellSegment struct {
	Raw       string
	Separator string
	Command   string
	Args      []string
}

func assessShellCommand(command string) shellAssessment {
	segments, err := splitShellSegments(command)
	if err != nil {
		return shellAssessment{
			Decision:         models.PolicyDecisionDeny,
			RuleID:           "complex_shell_syntax_deny",
			Category:         "shell",
			Severity:         "high",
			Reason:           "命令包含未支持或不安全的 shell 语法，已拒绝执行。",
			SaferAlternative: "请改成显式、扁平的命令，不要使用命令替换、后台执行、here-doc 或嵌套解释器。",
			OverrideAllowed:  false,
		}
	}
	if len(segments) == 0 {
		return shellAssessment{
			Decision:         models.PolicyDecisionDeny,
			RuleID:           "empty_shell_deny",
			Category:         "shell",
			Severity:         "medium",
			Reason:           "命令为空，已拒绝执行。",
			SaferAlternative: "请提供明确的命令。",
			OverrideAllowed:  false,
		}
	}

	for idx := range segments {
		words, err := splitShellWords(segments[idx].Raw)
		if err != nil {
			return shellAssessment{
				Decision:         models.PolicyDecisionDeny,
				RuleID:           "shell_parse_error_deny",
				Category:         "shell",
				Severity:         "medium",
				Reason:           "命令解析失败，已拒绝执行。",
				SaferAlternative: "请改用更直接的命令表达，避免复杂转义。",
				OverrideAllowed:  false,
			}
		}
		cmd, args := extractCommand(words)
		if cmd == "" {
			return shellAssessment{
				Decision:         models.PolicyDecisionDeny,
				RuleID:           "missing_executable_deny",
				Category:         "shell",
				Severity:         "medium",
				Reason:           "命令缺少可执行程序，已拒绝执行。",
				SaferAlternative: "请提供显式命令，不要只传环境变量或残缺片段。",
				OverrideAllowed:  false,
			}
		}
		if isNestedInterpreter(cmd, args) {
			return shellAssessment{
				Decision:         models.PolicyDecisionDeny,
				RuleID:           "nested_interpreter_deny",
				Category:         "shell",
				Severity:         "high",
				Reason:           fmt.Sprintf("命令尝试通过 %s 再次嵌套解释执行，已拒绝执行。", cmd),
				SaferAlternative: "请直接把要执行的命令展开成显式 run_shell，而不是再包一层解释器。",
				OverrideAllowed:  false,
			}
		}
		segments[idx].Command = cmd
		segments[idx].Args = args
	}

	for i := 0; i < len(segments)-1; i++ {
		if segments[i].Separator == "|" && isRemoteFetchCommand(segments[i].Command) && isShellProgram(segments[i+1].Command) {
			return shellAssessment{
				Decision:         models.PolicyDecisionDeny,
				RuleID:           "remote_download_pipe_shell_deny",
				Category:         "shell",
				Severity:         "critical",
				Reason:           "检测到远程下载内容直接通过管道交给 shell，已拒绝执行。",
				SaferAlternative: "请先单独下载并检查内容，再执行明确命令。",
				OverrideAllowed:  false,
			}
		}
	}

	for _, segment := range segments {
		if isDestructiveCommand(segment.Command, segment.Args, segment.Raw) {
			return shellAssessment{
				Decision:         models.PolicyDecisionDeny,
				RuleID:           "destructive_command_deny",
				Category:         "shell",
				Severity:         "critical",
				Reason:           fmt.Sprintf("命令包含高危或破坏性操作（%s），已拒绝执行。", segment.Command),
				SaferAlternative: "请先执行只读诊断命令缩小影响范围，再用审批内置动作或更小范围命令处理。",
				OverrideAllowed:  false,
			}
		}
	}

	allReadOnly := true
	for _, segment := range segments {
		if hasWriteRedirect(segment.Raw) {
			allReadOnly = false
			continue
		}
		if isReadOnlyCommand(segment.Command, segment.Args) {
			continue
		}
		allReadOnly = false
	}

	if allReadOnly {
		return shellAssessment{
			Decision:        models.PolicyDecisionAllow,
			RuleID:          "readonly_shell_allow",
			Category:        "shell",
			Severity:        "low",
			Reason:          "命令经安全解析后仅包含显式只读诊断步骤，允许直接执行。",
			OverrideAllowed: false,
		}
	}

	firstMutation := ""
	for _, segment := range segments {
		if hasWriteRedirect(segment.Raw) {
			firstMutation = firstNonEmpty(firstMutation, "shell redirection")
			break
		}
		if !isReadOnlyCommand(segment.Command, segment.Args) {
			firstMutation = firstNonEmpty(firstMutation, segment.Command)
			break
		}
	}

	return shellAssessment{
		Decision:         models.PolicyDecisionAsk,
		RuleID:           "mutating_shell_ask",
		Category:         "shell",
		Severity:         "high",
		Reason:           fmt.Sprintf("命令包含可能修改系统状态的步骤（%s），需人工确认后执行。", firstMutation),
		SaferAlternative: "优先拆成更小的只读 run_shell 诊断；若确需变更，请走审批执行。",
		OverrideAllowed:  true,
	}
}

func (e *Engine) newPolicyRule(ruleID, category, severity, decision, reason, scope, saferAlternative string, overrideAllowed bool) models.PolicyRule {
	e.mu.RLock()
	config, ok := e.rules[ruleID]
	e.mu.RUnlock()
	if ok {
		category = firstNonEmpty(config.Category, category)
		severity = firstNonEmpty(config.Severity, severity)
		decision = firstNonEmpty(config.Decision, decision)
		reason = firstNonEmpty(config.Reason, reason)
		saferAlternative = firstNonEmpty(config.SaferAlternative, saferAlternative)
		overrideAllowed = config.OverrideAllowed
	}
	return models.PolicyRule{
		RuleID:           ruleID,
		Category:         category,
		Severity:         severity,
		Decision:         decision,
		Reason:           reason,
		Scope:            scope,
		SaferAlternative: saferAlternative,
		OverrideAllowed:  overrideAllowed,
	}
}

func normalizeRuleConfigs(rules []models.PolicyRuleConfig) ([]models.PolicyRuleConfig, error) {
	defaults := make(map[string]models.PolicyRuleConfig)
	for _, item := range DefaultRuleConfigs() {
		defaults[item.ID] = item
	}
	seen := make(map[string]bool)
	merged := make([]models.PolicyRuleConfig, 0, len(defaults))
	for _, item := range rules {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			return nil, fmt.Errorf("policy rule id is required")
		}
		base, found := defaults[item.ID]
		if !found {
			return nil, fmt.Errorf("unknown policy rule id: %s", item.ID)
		}
		item.Category = firstNonEmpty(strings.TrimSpace(item.Category), base.Category)
		item.Severity = firstNonEmpty(strings.TrimSpace(item.Severity), base.Severity)
		item.Decision = firstNonEmpty(strings.TrimSpace(item.Decision), base.Decision)
		item.Reason = strings.TrimSpace(item.Reason)
		item.SaferAlternative = strings.TrimSpace(item.SaferAlternative)
		item.Description = firstNonEmpty(strings.TrimSpace(item.Description), base.Description)
		if isProtectedDenyRule(item.ID) && item.Decision != models.PolicyDecisionDeny {
			return nil, fmt.Errorf("protected deny rule %s cannot be relaxed", item.ID)
		}
		if !isKnownDecision(item.Decision) {
			return nil, fmt.Errorf("unsupported policy decision for %s: %s", item.ID, item.Decision)
		}
		if isProtectedDenyRule(item.ID) {
			item.OverrideAllowed = false
		}
		if item.UpdatedAt.IsZero() {
			item.UpdatedAt = time.Now().UTC()
		}
		seen[item.ID] = true
		merged = append(merged, item)
	}
	for _, item := range DefaultRuleConfigs() {
		if seen[item.ID] {
			continue
		}
		merged = append(merged, item)
	}
	return sortRuleConfigs(merged), nil
}

func sortRuleConfigs(items []models.PolicyRuleConfig) []models.PolicyRuleConfig {
	order := make(map[string]int)
	for index, item := range DefaultRuleConfigs() {
		order[item.ID] = index
	}
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if order[items[j].ID] < order[items[i].ID] {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	return items
}

func isProtectedDenyRule(id string) bool {
	switch id {
	case "destructive_command_deny", "remote_download_pipe_shell_deny", "nested_interpreter_deny", "complex_shell_syntax_deny", "empty_shell_deny", "shell_parse_error_deny", "missing_executable_deny":
		return true
	default:
		return false
	}
}

func isKnownDecision(value string) bool {
	return value == models.PolicyDecisionAllow || value == models.PolicyDecisionAsk || value == models.PolicyDecisionDeny
}

func splitShellSegments(command string) ([]shellSegment, error) {
	var segments []shellSegment
	var current strings.Builder
	state := shellScanNormal

	flush := func(separator string) error {
		raw := strings.TrimSpace(current.String())
		if raw == "" {
			return fmt.Errorf("empty shell segment")
		}
		segments = append(segments, shellSegment{Raw: raw, Separator: separator})
		current.Reset()
		return nil
	}

	for i := 0; i < len(command); i++ {
		ch := command[i]
		switch state {
		case shellScanSingleQuote:
			current.WriteByte(ch)
			if ch == '\'' {
				state = shellScanNormal
			}
			continue
		case shellScanDoubleQuote:
			current.WriteByte(ch)
			if ch == '\\' {
				if i+1 >= len(command) {
					return nil, fmt.Errorf("unterminated escape")
				}
				i++
				current.WriteByte(command[i])
				continue
			}
			if ch == '"' {
				state = shellScanNormal
			}
			continue
		}

		switch ch {
		case '\'':
			state = shellScanSingleQuote
			current.WriteByte(ch)
		case '"':
			state = shellScanDoubleQuote
			current.WriteByte(ch)
		case '`':
			return nil, fmt.Errorf("backticks are not allowed")
		case '$':
			if i+1 < len(command) && command[i+1] == '(' {
				return nil, fmt.Errorf("command substitution is not allowed")
			}
			current.WriteByte(ch)
		case '<':
			if i+1 < len(command) {
				switch command[i+1] {
				case '<':
					return nil, fmt.Errorf("heredoc is not allowed")
				case '(':
					return nil, fmt.Errorf("process substitution is not allowed")
				}
			}
			current.WriteByte(ch)
		case '>':
			if i+1 < len(command) && command[i+1] == '(' {
				return nil, fmt.Errorf("process substitution is not allowed")
			}
			current.WriteByte(ch)
		case '&':
			if previousNonSpaceByte(current.String()) == '>' {
				current.WriteByte(ch)
				continue
			}
			if i+1 < len(command) && command[i+1] == '&' {
				if err := flush("&&"); err != nil {
					return nil, err
				}
				i++
				continue
			}
			return nil, fmt.Errorf("background execution is not allowed")
		case '|':
			if i+1 < len(command) && command[i+1] == '|' {
				if err := flush("||"); err != nil {
					return nil, err
				}
				i++
				continue
			}
			if err := flush("|"); err != nil {
				return nil, err
			}
		case ';', '\n':
			if err := flush(";"); err != nil {
				return nil, err
			}
		default:
			current.WriteByte(ch)
		}
	}

	if state != shellScanNormal {
		return nil, fmt.Errorf("unterminated quotes")
	}

	tail := strings.TrimSpace(current.String())
	if tail == "" {
		if len(segments) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("trailing separator")
	}
	segments = append(segments, shellSegment{Raw: tail})
	return segments, nil
}

func splitShellWords(segment string) ([]string, error) {
	var words []string
	var current strings.Builder
	state := shellScanNormal

	flush := func() {
		if current.Len() == 0 {
			return
		}
		words = append(words, current.String())
		current.Reset()
	}

	for i := 0; i < len(segment); i++ {
		ch := segment[i]
		switch state {
		case shellScanSingleQuote:
			if ch == '\'' {
				state = shellScanNormal
				continue
			}
			current.WriteByte(ch)
			continue
		case shellScanDoubleQuote:
			if ch == '\\' {
				if i+1 >= len(segment) {
					return nil, fmt.Errorf("unterminated escape")
				}
				i++
				current.WriteByte(segment[i])
				continue
			}
			if ch == '"' {
				state = shellScanNormal
				continue
			}
			current.WriteByte(ch)
			continue
		}

		switch {
		case unicode.IsSpace(rune(ch)):
			flush()
		case ch == '\'':
			state = shellScanSingleQuote
		case ch == '"':
			state = shellScanDoubleQuote
		case ch == '\\':
			if i+1 >= len(segment) {
				return nil, fmt.Errorf("unterminated escape")
			}
			i++
			current.WriteByte(segment[i])
		default:
			current.WriteByte(ch)
		}
	}

	if state != shellScanNormal {
		return nil, fmt.Errorf("unterminated quotes")
	}
	flush()
	return words, nil
}

func extractCommand(words []string) (string, []string) {
	for i, word := range words {
		if assignmentPattern.MatchString(word) {
			continue
		}
		return strings.ToLower(word), words[i+1:]
	}
	return "", nil
}

func hasWriteRedirect(segment string) bool {
	state := shellScanNormal
	for i := 0; i < len(segment); i++ {
		ch := segment[i]
		switch state {
		case shellScanSingleQuote:
			if ch == '\'' {
				state = shellScanNormal
			}
			continue
		case shellScanDoubleQuote:
			if ch == '\\' {
				i++
				continue
			}
			if ch == '"' {
				state = shellScanNormal
			}
			continue
		}

		switch ch {
		case '\'':
			state = shellScanSingleQuote
		case '"':
			state = shellScanDoubleQuote
		case '>':
			if i+1 < len(segment) && segment[i+1] == '>' {
				i++
			}
			j := i + 1
			for j < len(segment) && unicode.IsSpace(rune(segment[j])) {
				j++
			}
			start := j
			for j < len(segment) && !unicode.IsSpace(rune(segment[j])) && !isSegmentSeparator(segment[j]) {
				j++
			}
			target := segment[start:j]
			if !isBenignRedirectTarget(target) {
				return true
			}
		}
	}
	return false
}

func isReadOnlyCommand(command string, args []string) bool {
	switch command {
	case "hostname", "uname", "whoami", "pwd", "date", "df", "du", "free", "vm_stat", "swapon", "swapinfo", "lsblk", "mount",
		"ps", "pgrep", "top", "vmstat", "iostat", "sar", "ss", "netstat", "lsof", "cat", "less", "more", "tail", "head",
		"grep", "egrep", "fgrep", "awk", "find", "ls", "stat", "file", "readlink", "realpath", "id", "getent", "groups",
		"journalctl", "dmesg", "printenv", "which", "type", "wc", "sort", "uniq", "cut", "tr", "basename", "dirname",
		"uptime", "echo", "printf":
		return true
	case "env":
		return len(args) == 0
	case "sed":
		return hasArg(args, "-n") && !hasArgWithPrefix(args, "-i")
	case "sysctl":
		return !containsEquals(args)
	case "systemctl":
		return len(args) > 0 && readOnlySystemctlArgs[strings.ToLower(args[0])]
	case "service":
		return len(args) >= 2 && strings.EqualFold(args[1], "status")
	case "launchctl":
		return len(args) > 0 && readOnlyLaunchctlArgs[strings.ToLower(args[0])]
	case "apt-cache":
		return true
	case "apt":
		return len(args) > 0 && readOnlyAptArgs[strings.ToLower(args[0])]
	case "dnf", "yum":
		return len(args) > 0 && readOnlyDnfArgs[strings.ToLower(args[0])]
	case "rpm":
		return len(args) > 0 && args[0] == "-q"
	case "dpkg":
		return len(args) > 0 && args[0] == "-s"
	case "zypper":
		return len(args) > 0 && strings.EqualFold(args[0], "search")
	case "pacman":
		return len(args) > 0 && args[0] == "-Q"
	case "command":
		return len(args) > 0 && (args[0] == "-v" || args[0] == "-V")
	case "dscl":
		return len(args) >= 2 && args[1] == "-read"
	default:
		return false
	}
}

func isDestructiveCommand(command string, args []string, raw string) bool {
	switch command {
	case "mkfs", "shutdown", "reboot", "poweroff", "halt", "init", "fsck":
		return true
	case "dd":
		return true
	case "rm":
		normalized := " " + strings.ToLower(raw) + " "
		return strings.Contains(normalized, " rm -rf /") || strings.Contains(normalized, " rm -fr /") || hasArg(args, "/") || hasArg(args, "/*")
	case "curl", "wget":
		return false
	}
	return destructiveCommandPattern.MatchString(command)
}

func isRemoteFetchCommand(command string) bool {
	return command == "curl" || command == "wget"
}

func isShellProgram(command string) bool {
	return command == "sh" || command == "bash" || command == "zsh"
}

func isNestedInterpreter(command string, args []string) bool {
	switch command {
	case "sh", "bash", "zsh":
		return hasArg(args, "-c")
	case "python", "python3", "perl", "ruby", "node":
		return hasArg(args, "-c") || hasArg(args, "-e")
	default:
		return false
	}
}

func hasArg(args []string, target string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, target) {
			return true
		}
	}
	return false
}

func hasArgWithPrefix(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(arg), strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

func containsEquals(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "=") {
			return true
		}
	}
	return false
}

func isBenignRedirectTarget(target string) bool {
	target = strings.TrimSpace(target)
	switch target {
	case "", "/dev/null", "1", "2", "&1", "&2":
		return true
	default:
		return false
	}
}

func isSegmentSeparator(ch byte) bool {
	switch ch {
	case ';', '|', '&', '<', '>':
		return true
	default:
		return false
	}
}

func previousNonSpaceByte(value string) byte {
	for i := len(value) - 1; i >= 0; i-- {
		if !unicode.IsSpace(rune(value[i])) {
			return value[i]
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type shellScanState int

const (
	shellScanNormal shellScanState = iota
	shellScanSingleQuote
	shellScanDoubleQuote
)

var (
	assignmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=.*$`)

	readOnlySystemctlArgs = map[string]bool{
		"status":          true,
		"show":            true,
		"list-units":      true,
		"list-unit-files": true,
		"cat":             true,
	}
	readOnlyLaunchctlArgs = map[string]bool{
		"list":  true,
		"print": true,
	}
	readOnlyAptArgs = map[string]bool{
		"list":   true,
		"show":   true,
		"policy": true,
	}
	readOnlyDnfArgs = map[string]bool{
		"list":     true,
		"info":     true,
		"repolist": true,
	}
	destructiveCommandPattern = regexp.MustCompile(`^(wipefs|sfdisk|fdisk|parted|cryptsetup)$`)
)
