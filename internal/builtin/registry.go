package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"osagentmvp/internal/models"
	"osagentmvp/internal/runner"
)

type ExecutionContext struct {
	Runner *runner.Executor
	Stream runner.StreamSink
}

type Tool interface {
	Definition() models.ToolDefinition
	Summary() models.ToolSummary
	Preview(arguments string) (models.ActionPreview, error)
	Execute(context.Context, models.Host, ExecutionContext, string) (string, error)
}

type Registry struct {
	executor *runner.Executor
	tools    map[string]Tool
}

func NewRegistry(executor *runner.Executor) *Registry {
	registry := &Registry{
		executor: executor,
		tools:    make(map[string]Tool),
	}
	for _, tool := range []Tool{
		newStaticTool(),
		newReadOnlyCommandTool("host_probe", "Collect hostname, kernel, distro, shell, init system and key command presence.", true, "hostname; printf '\\n'; uname -a; printf '\\n'; if [ -f /etc/os-release ]; then cat /etc/os-release; elif command -v sw_vers >/dev/null 2>&1; then sw_vers; fi; printf '\\n'; ps -p 1 -o comm=; printf '\\n'; command -v systemctl || true; command -v service || true; command -v ss || true; command -v netstat || true; command -v journalctl || true; command -v apt || true; command -v dnf || true; command -v yum || true; command -v zypper || true"),
		newReadOnlyCommandTool("memory_inspect", "Inspect memory and swap pressure using Linux or Darwin native commands.", true, "free -h 2>/dev/null || vm_stat; printf '\\n'; cat /proc/meminfo 2>/dev/null | head -n 20 || sysctl vm.swapusage 2>/dev/null || true"),
		newReadOnlyCommandTool("disk_inspect", "Inspect filesystem usage and inode pressure.", true, "df -h; printf '\\n'; df -i"),
		newParameterizedReadTool("port_inspect", "Inspect listening or connected ports. Optional port filter.", `ss -ltnp 2>/dev/null || netstat -ltnp 2>/dev/null`, "port"),
		newProcessSearchTool(),
		newServiceStatusTool(),
		newFileLogSearchTool(),
		newDirectoryUsageTool(),
		newJournalLogSearchTool(),
		newPackageManagerInspectTool(),
		newUserInspectTool(),
		newMutatingTool("create_user", "Create a standard Linux user account.", "High-risk identity change; requires approval."),
		newDeleteUserTool(),
		newMutatingTool("restart_service", "Restart a service after approval.", "Service restart may affect availability; requires approval."),
		newShellTool(),
	} {
		registry.tools[tool.Definition().Function.Name] = tool
	}
	return registry
}

func (r *Registry) Definitions() []models.ToolDefinition {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	defs := make([]models.ToolDefinition, 0, len(r.tools))
	for _, name := range names {
		defs = append(defs, r.tools[name].Definition())
	}
	return defs
}

func (r *Registry) Summaries() []models.ToolSummary {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]models.ToolSummary, 0, len(r.tools))
	for _, name := range names {
		items = append(items, r.tools[name].Summary())
	}
	return items
}

func (r *Registry) Preview(call models.ToolCall) (models.ActionPreview, error) {
	tool, ok := r.tools[call.Function.Name]
	if !ok {
		return models.ActionPreview{}, fmt.Errorf("unknown tool %q", call.Function.Name)
	}
	return tool.Preview(call.Function.Arguments)
}

func (r *Registry) Execute(ctx context.Context, host models.Host, call models.ToolCall, stream runner.StreamSink) (string, error) {
	tool, ok := r.tools[call.Function.Name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", call.Function.Name)
	}
	return tool.Execute(ctx, host, ExecutionContext{Runner: r.executor, Stream: stream}, call.Function.Arguments)
}

type staticTool struct{}

func newStaticTool() Tool { return &staticTool{} }

func (t *staticTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        "hello_capability",
			Description: "Explain what this OS agent can do and how it handles risk.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}
func (t *staticTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: "hello_capability", Description: "Explain capabilities and safety boundaries.", ReadOnly: true}
}
func (t *staticTool) Preview(string) (models.ActionPreview, error) {
	return models.ActionPreview{ToolName: "hello_capability", ReadOnly: true}, nil
}
func (t *staticTool) Execute(context.Context, models.Host, ExecutionContext, string) (string, error) {
	return "I can inspect host facts, memory, disk, ports, processes, services, package managers, users, journals, directory usage, and logs. I can also create users, delete users, restart services, or run custom shell commands, but mutating operations require approval and destructive shell commands are denied.", nil
}

type readOnlyCommandTool struct {
	name        string
	description string
	command     string
}

func newReadOnlyCommandTool(name, description string, _ bool, command string) Tool {
	return &readOnlyCommandTool{name: name, description: description, command: command}
}

func (t *readOnlyCommandTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        t.name,
			Description: t.description,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}
func (t *readOnlyCommandTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: t.name, Description: t.description, ReadOnly: true}
}
func (t *readOnlyCommandTool) Preview(string) (models.ActionPreview, error) {
	return models.ActionPreview{ToolName: t.name, ReadOnly: true, CommandPreview: t.command}, nil
}
func (t *readOnlyCommandTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, _ string) (string, error) {
	result, err := execCtx.Runner.Run(ctx, host, t.command, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

type parameterizedReadTool struct {
	name        string
	description string
	baseCommand string
	filterKey   string
}

func newParameterizedReadTool(name, description, baseCommand, filterKey string) Tool {
	return &parameterizedReadTool{name: name, description: description, baseCommand: baseCommand, filterKey: filterKey}
}
func (t *parameterizedReadTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        t.name,
			Description: t.description,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					t.filterKey: map[string]any{
						"type":        "string",
						"description": "Optional filter value.",
					},
				},
			},
		},
	}
}
func (t *parameterizedReadTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: t.name, Description: t.description, ReadOnly: true}
}
func (t *parameterizedReadTool) Preview(arguments string) (models.ActionPreview, error) {
	command := t.baseCommand
	if filter, err := stringArg(arguments, t.filterKey); err == nil && strings.TrimSpace(filter) != "" {
		command += " | grep -n -- " + shellQuote(filter)
	}
	return models.ActionPreview{ToolName: t.name, ReadOnly: true, CommandPreview: command}, nil
}
func (t *parameterizedReadTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, arguments string) (string, error) {
	preview, err := t.Preview(arguments)
	if err != nil {
		return "", err
	}
	result, err := execCtx.Runner.Run(ctx, host, preview.CommandPreview, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

type processSearchTool struct{}

func newProcessSearchTool() Tool { return &processSearchTool{} }
func (t *processSearchTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        "process_search",
			Description: "Search process list by keyword or command fragment.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]any{"type": "string", "description": "Process keyword to search."},
				},
				"required": []string{"keyword"},
			},
		},
	}
}
func (t *processSearchTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: "process_search", Description: "Search running processes.", ReadOnly: true}
}
func (t *processSearchTool) Preview(arguments string) (models.ActionPreview, error) {
	keyword, err := stringArg(arguments, "keyword")
	if err != nil {
		return models.ActionPreview{}, err
	}
	return models.ActionPreview{ToolName: "process_search", ReadOnly: true, CommandPreview: "ps -ef | grep -i -- " + shellQuote(keyword) + " | grep -v grep"}, nil
}
func (t *processSearchTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, arguments string) (string, error) {
	preview, err := t.Preview(arguments)
	if err != nil {
		return "", err
	}
	result, err := execCtx.Runner.Run(ctx, host, preview.CommandPreview, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

type serviceStatusTool struct{}

func newServiceStatusTool() Tool { return &serviceStatusTool{} }
func (t *serviceStatusTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        "service_status_inspect",
			Description: "Inspect Linux service status with systemctl or service.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service": map[string]any{"type": "string", "description": "Service name."},
				},
				"required": []string{"service"},
			},
		},
	}
}
func (t *serviceStatusTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: "service_status_inspect", Description: "Inspect service status.", ReadOnly: true}
}
func (t *serviceStatusTool) Preview(arguments string) (models.ActionPreview, error) {
	service, err := stringArg(arguments, "service")
	if err != nil {
		return models.ActionPreview{}, err
	}
	command := "systemctl status --no-pager " + shellQuote(service) + " 2>/dev/null || service " + shellQuote(service) + " status 2>/dev/null || launchctl list | grep -i -- " + shellQuote(service)
	return models.ActionPreview{ToolName: "service_status_inspect", ReadOnly: true, CommandPreview: command}, nil
}
func (t *serviceStatusTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, arguments string) (string, error) {
	preview, err := t.Preview(arguments)
	if err != nil {
		return "", err
	}
	result, err := execCtx.Runner.Run(ctx, host, preview.CommandPreview, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

type fileLogSearchTool struct{}

func newFileLogSearchTool() Tool { return &fileLogSearchTool{} }
func (t *fileLogSearchTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        "file_log_search",
			Description: "Search a file or log path for a pattern and show recent context.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Log or file path."},
					"pattern": map[string]any{"type": "string", "description": "Search pattern."},
				},
				"required": []string{"path", "pattern"},
			},
		},
	}
}
func (t *fileLogSearchTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: "file_log_search", Description: "Search log/file contents.", ReadOnly: true}
}
func (t *fileLogSearchTool) Preview(arguments string) (models.ActionPreview, error) {
	var input struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := decodeArguments(arguments, &input); err != nil {
		return models.ActionPreview{}, err
	}
	command := "grep -n -C 2 -- " + shellQuote(input.Pattern) + " " + shellQuote(input.Path) + " | tail -n 80"
	return models.ActionPreview{ToolName: "file_log_search", ReadOnly: true, CommandPreview: command}, nil
}
func (t *fileLogSearchTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, arguments string) (string, error) {
	preview, err := t.Preview(arguments)
	if err != nil {
		return "", err
	}
	result, err := execCtx.Runner.Run(ctx, host, preview.CommandPreview, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

type directoryUsageTool struct{}

func newDirectoryUsageTool() Tool { return &directoryUsageTool{} }
func (t *directoryUsageTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        "directory_usage_inspect",
			Description: "Inspect directory size hotspots for a target path.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Directory path to inspect."},
				},
				"required": []string{"path"},
			},
		},
	}
}
func (t *directoryUsageTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: "directory_usage_inspect", Description: "Inspect per-directory disk usage.", ReadOnly: true}
}
func (t *directoryUsageTool) Preview(arguments string) (models.ActionPreview, error) {
	path, err := stringArg(arguments, "path")
	if err != nil {
		return models.ActionPreview{}, err
	}
	command := "du -xh -d 1 " + shellQuote(path) + " 2>/dev/null || du -xh -d 1 " + shellQuote(path) + " || du -sh " + shellQuote(path)
	return models.ActionPreview{ToolName: "directory_usage_inspect", ReadOnly: true, CommandPreview: command}, nil
}
func (t *directoryUsageTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, arguments string) (string, error) {
	preview, err := t.Preview(arguments)
	if err != nil {
		return "", err
	}
	result, err := execCtx.Runner.Run(ctx, host, preview.CommandPreview, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

type journalLogSearchTool struct{}

func newJournalLogSearchTool() Tool { return &journalLogSearchTool{} }
func (t *journalLogSearchTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        "journal_log_search",
			Description: "Read recent journal or syslog records for a service.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"service": map[string]any{"type": "string", "description": "Service or unit name."},
					"lines":   map[string]any{"type": "integer", "description": "Optional number of lines, default 80."},
				},
				"required": []string{"service"},
			},
		},
	}
}
func (t *journalLogSearchTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: "journal_log_search", Description: "Inspect recent journal/syslog lines for a service.", ReadOnly: true}
}
func (t *journalLogSearchTool) Preview(arguments string) (models.ActionPreview, error) {
	var input struct {
		Service string `json:"service"`
		Lines   int    `json:"lines"`
	}
	if err := decodeArguments(arguments, &input); err != nil {
		return models.ActionPreview{}, err
	}
	if strings.TrimSpace(input.Service) == "" {
		return models.ActionPreview{}, fmt.Errorf("service is required")
	}
	if input.Lines <= 0 {
		input.Lines = 80
	}
	command := fmt.Sprintf("journalctl -u %s -n %d --no-pager 2>/dev/null || grep -i -- %s /var/log/system.log 2>/dev/null | tail -n %d || grep -i -- %s /var/log/messages 2>/dev/null | tail -n %d", shellQuote(input.Service), input.Lines, shellQuote(input.Service), input.Lines, shellQuote(input.Service), input.Lines)
	return models.ActionPreview{ToolName: "journal_log_search", ReadOnly: true, CommandPreview: command}, nil
}
func (t *journalLogSearchTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, arguments string) (string, error) {
	preview, err := t.Preview(arguments)
	if err != nil {
		return "", err
	}
	result, err := execCtx.Runner.Run(ctx, host, preview.CommandPreview, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

type packageManagerInspectTool struct{}

func newPackageManagerInspectTool() Tool { return &packageManagerInspectTool{} }
func (t *packageManagerInspectTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        "package_manager_inspect",
			Description: "Inspect Linux package manager availability and optionally query a package.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"package": map[string]any{"type": "string", "description": "Optional package name to query."},
				},
			},
		},
	}
}
func (t *packageManagerInspectTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: "package_manager_inspect", Description: "Inspect distro package manager and package state.", ReadOnly: true}
}
func (t *packageManagerInspectTool) Preview(arguments string) (models.ActionPreview, error) {
	command := "command -v apt || true; command -v dnf || true; command -v yum || true; command -v zypper || true; command -v pacman || true"
	if pkg, err := stringArg(arguments, "package"); err == nil && strings.TrimSpace(pkg) != "" {
		command += "; dpkg -s " + shellQuote(pkg) + " 2>/dev/null || rpm -qi " + shellQuote(pkg) + " 2>/dev/null || pacman -Qi " + shellQuote(pkg) + " 2>/dev/null || true"
	}
	return models.ActionPreview{ToolName: "package_manager_inspect", ReadOnly: true, CommandPreview: command}, nil
}
func (t *packageManagerInspectTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, arguments string) (string, error) {
	preview, err := t.Preview(arguments)
	if err != nil {
		return "", err
	}
	result, err := execCtx.Runner.Run(ctx, host, preview.CommandPreview, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

type userInspectTool struct{}

func newUserInspectTool() Tool { return &userInspectTool{} }
func (t *userInspectTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        "user_inspect",
			Description: "Inspect user existence, uid/gid, groups, shell, and passwd entry.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"username": map[string]any{"type": "string", "description": "Username to inspect."},
				},
				"required": []string{"username"},
			},
		},
	}
}
func (t *userInspectTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: "user_inspect", Description: "Inspect Linux user account facts.", ReadOnly: true}
}
func (t *userInspectTool) Preview(arguments string) (models.ActionPreview, error) {
	username, err := stringArg(arguments, "username")
	if err != nil {
		return models.ActionPreview{}, err
	}
	command := "id " + shellQuote(username) + " 2>/dev/null; getent passwd " + shellQuote(username) + " 2>/dev/null || dscl . -read /Users/" + shellQuote(username) + " 2>/dev/null || true"
	return models.ActionPreview{ToolName: "user_inspect", ReadOnly: true, CommandPreview: command}, nil
}
func (t *userInspectTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, arguments string) (string, error) {
	preview, err := t.Preview(arguments)
	if err != nil {
		return "", err
	}
	result, err := execCtx.Runner.Run(ctx, host, preview.CommandPreview, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

type mutatingTool struct {
	name        string
	description string
	riskHint    string
}

func newMutatingTool(name, description, riskHint string) Tool {
	return &mutatingTool{name: name, description: description, riskHint: riskHint}
}
func (t *mutatingTool) Definition() models.ToolDefinition {
	description := t.description
	switch t.name {
	case "create_user":
		return models.ToolDefinition{
			Type: "function",
			Function: models.ToolFunctionDefinition{
				Name:        t.name,
				Description: description,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"username": map[string]any{"type": "string", "description": "Username to create."},
						"shell":    map[string]any{"type": "string", "description": "Optional login shell."},
					},
					"required": []string{"username"},
				},
			},
		}
	case "restart_service":
		return models.ToolDefinition{
			Type: "function",
			Function: models.ToolFunctionDefinition{
				Name:        t.name,
				Description: description,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"service": map[string]any{"type": "string", "description": "Service name to restart."},
					},
					"required": []string{"service"},
				},
			},
		}
	default:
		panic("unsupported mutating tool")
	}
}
func (t *mutatingTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: t.name, Description: t.description, ReadOnly: false}
}
func (t *mutatingTool) Preview(arguments string) (models.ActionPreview, error) {
	switch t.name {
	case "create_user":
		var input struct {
			Username string `json:"username"`
			Shell    string `json:"shell"`
		}
		if err := decodeArguments(arguments, &input); err != nil {
			return models.ActionPreview{}, err
		}
		command := "id " + shellQuote(input.Username) + " >/dev/null 2>&1 || useradd"
		if strings.TrimSpace(input.Shell) != "" {
			command += " -s " + shellQuote(input.Shell)
		}
		command += " " + shellQuote(input.Username)
		return models.ActionPreview{ToolName: t.name, ReadOnly: false, CommandPreview: command, RiskHint: t.riskHint}, nil
	case "restart_service":
		service, err := stringArg(arguments, "service")
		if err != nil {
			return models.ActionPreview{}, err
		}
		command := "systemctl restart " + shellQuote(service) + " || service " + shellQuote(service) + " restart"
		return models.ActionPreview{ToolName: t.name, ReadOnly: false, CommandPreview: command, RiskHint: t.riskHint}, nil
	default:
		return models.ActionPreview{}, errors.New("unsupported mutating tool")
	}
}
func (t *mutatingTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, arguments string) (string, error) {
	preview, err := t.Preview(arguments)
	if err != nil {
		return "", err
	}
	result, err := execCtx.Runner.Run(ctx, host, preview.CommandPreview, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

type deleteUserTool struct{}

func newDeleteUserTool() Tool { return &deleteUserTool{} }
func (t *deleteUserTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        "delete_user",
			Description: "Delete a Linux user account after approval.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"username":    map[string]any{"type": "string", "description": "Username to delete."},
					"remove_home": map[string]any{"type": "boolean", "description": "Also remove the home directory."},
				},
				"required": []string{"username"},
			},
		},
	}
}
func (t *deleteUserTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: "delete_user", Description: "Delete a Linux user account.", ReadOnly: false}
}
func (t *deleteUserTool) Preview(arguments string) (models.ActionPreview, error) {
	var input struct {
		Username   string `json:"username"`
		RemoveHome bool   `json:"remove_home"`
	}
	if err := decodeArguments(arguments, &input); err != nil {
		return models.ActionPreview{}, err
	}
	command := "userdel"
	if input.RemoveHome {
		command += " -r"
	}
	command += " " + shellQuote(input.Username)
	return models.ActionPreview{ToolName: "delete_user", ReadOnly: false, CommandPreview: command, RiskHint: "User deletion changes identity state; requires approval."}, nil
}
func (t *deleteUserTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, arguments string) (string, error) {
	preview, err := t.Preview(arguments)
	if err != nil {
		return "", err
	}
	result, err := execCtx.Runner.Run(ctx, host, preview.CommandPreview, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

type shellTool struct{}

func newShellTool() Tool { return &shellTool{} }
func (t *shellTool) Definition() models.ToolDefinition {
	return models.ToolDefinition{
		Type: "function",
		Function: models.ToolFunctionDefinition{
			Name:        "run_shell",
			Description: "Run a shell command when no builtin tool covers the task. This always goes through policy.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "Shell command to execute."},
				},
				"required": []string{"command"},
			},
		},
	}
}
func (t *shellTool) Summary() models.ToolSummary {
	return models.ToolSummary{Name: "run_shell", Description: "Run a custom shell command through policy.", ReadOnly: false}
}
func (t *shellTool) Preview(arguments string) (models.ActionPreview, error) {
	command, err := stringArg(arguments, "command")
	if err != nil {
		return models.ActionPreview{}, err
	}
	return models.ActionPreview{ToolName: "run_shell", ReadOnly: false, CommandPreview: command, RiskHint: "Custom shell execution can be dangerous; requires policy review."}, nil
}
func (t *shellTool) Execute(ctx context.Context, host models.Host, execCtx ExecutionContext, arguments string) (string, error) {
	preview, err := t.Preview(arguments)
	if err != nil {
		return "", err
	}
	result, err := execCtx.Runner.Run(ctx, host, preview.CommandPreview, execCtx.Stream)
	return strings.TrimSpace(result.Stdout + "\n" + result.Stderr), err
}

func decodeArguments(arguments string, target any) error {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		trimmed = "{}"
	}
	if err := json.Unmarshal([]byte(trimmed), target); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	return nil
}

func stringArg(arguments string, key string) (string, error) {
	var input map[string]any
	if err := decodeArguments(arguments, &input); err != nil {
		return "", err
	}
	value, _ := input[key].(string)
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
