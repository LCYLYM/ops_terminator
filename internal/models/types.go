package models

import "time"

const (
	HostModeLocal = "local"
	HostModeSSH   = "ssh"
)

const (
	RunStatusCreated         = "created"
	RunStatusRunningAgent    = "running_agent"
	RunStatusWaitingApproval = "waiting_approval"
	RunStatusToolRunning     = "tool_running"
	RunStatusCompleted       = "completed"
	RunStatusDenied          = "denied"
	RunStatusFailed          = "failed"
)

const (
	PolicyDecisionAllow = "allow"
	PolicyDecisionAsk   = "ask"
	PolicyDecisionDeny  = "deny"
)

type Host struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"display_name"`
	Mode        string            `json:"mode"`
	Address     string            `json:"address,omitempty"`
	Port        int               `json:"port,omitempty"`
	User        string            `json:"user,omitempty"`
	PasswordEnv string            `json:"password_env,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type Session struct {
	ID          string    `json:"id"`
	HostID      string    `json:"host_id"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary,omitempty"`
	TurnIDs     []string  `json:"turn_ids,omitempty"`
	RunIDs      []string  `json:"run_ids,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastInput   string    `json:"last_input,omitempty"`
	LastOutcome string    `json:"last_outcome,omitempty"`
}

type Turn struct {
	ID               string          `json:"id"`
	SessionID        string          `json:"session_id"`
	HostID           string          `json:"host_id"`
	UserInput        string          `json:"user_input"`
	ContextSnapshot  ContextSnapshot `json:"context_snapshot"`
	FinalExplanation string          `json:"final_explanation,omitempty"`
	RunID            string          `json:"run_id"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type Run struct {
	ID              string       `json:"id"`
	SessionID       string       `json:"session_id"`
	TurnID          string       `json:"turn_id"`
	HostID          string       `json:"host_id"`
	Status          string       `json:"status"`
	PendingApproval string       `json:"pending_approval,omitempty"`
	ToolHistory     []string     `json:"tool_history,omitempty"`
	PolicyHistory   []PolicyRule `json:"policy_history,omitempty"`
	FinalResponse   string       `json:"final_response,omitempty"`
	FailureMessage  string       `json:"failure_message,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	CompletedAt     *time.Time   `json:"completed_at,omitempty"`
}

type Approval struct {
	ID               string     `json:"id"`
	RunID            string     `json:"run_id"`
	ToolName         string     `json:"tool_name"`
	Reason           string     `json:"reason"`
	Scope            string     `json:"scope"`
	SaferAlternative string     `json:"safer_alternative,omitempty"`
	RequestedBy      string     `json:"requested_by"`
	Decision         string     `json:"decision,omitempty"`
	ResolvedBy       string     `json:"resolved_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
}

type Event struct {
	ID        string         `json:"id"`
	RunID     string         `json:"run_id"`
	Type      string         `json:"type"`
	Message   string         `json:"message,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

type AuditEntry struct {
	ID        string         `json:"id"`
	RunID     string         `json:"run_id"`
	Kind      string         `json:"kind"`
	Summary   string         `json:"summary"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type SkillDefinition struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	IntentExamples    []string `json:"intent_examples"`
	RiskCategory      string   `json:"risk_category"`
	RecommendedFlow   []string `json:"recommended_investigation_flow"`
	DecisionHints     []string `json:"decision_hints"`
	ExplanationHints  []string `json:"explanation_hints,omitempty"`
	SaferAlternatives []string `json:"safer_alternatives,omitempty"`
}

type SkillSummary struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	RiskCategory string   `json:"risk_category"`
	Examples     []string `json:"examples"`
}

type ContextSnapshot struct {
	HostID           string         `json:"host_id"`
	HostDisplayName  string         `json:"host_display_name"`
	HostMode         string         `json:"host_mode"`
	SessionSummary   string         `json:"session_summary,omitempty"`
	PolicySummary    string         `json:"policy_summary"`
	SkillSummaries   []SkillSummary `json:"skill_summaries,omitempty"`
	BuiltinSummaries []ToolSummary  `json:"builtin_summaries,omitempty"`
}

type ToolSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReadOnly    bool   `json:"read_only"`
}

type ToolFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ToolDefinition struct {
	Type     string                 `json:"type"`
	Function ToolFunctionDefinition `json:"function"`
}

type ToolFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolFunctionCall `json:"function"`
}

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

type AssistantResponse struct {
	ID           string
	Model        string
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        *Usage
}

type ActionPreview struct {
	ToolName       string `json:"tool_name"`
	ReadOnly       bool   `json:"read_only"`
	CommandPreview string `json:"command_preview,omitempty"`
	RiskHint       string `json:"risk_hint,omitempty"`
}

type PolicyRule struct {
	Decision         string `json:"decision"`
	Reason           string `json:"reason"`
	Scope            string `json:"scope"`
	SaferAlternative string `json:"safer_alternative,omitempty"`
}
