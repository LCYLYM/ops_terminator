package models

import "time"

const (
	HostModeLocal = "local"
	HostModeSSH   = "ssh"
)

const (
	ApprovalDecisionApprove      = "approve"
	ApprovalDecisionReject       = "reject"
	ApprovalDecisionForceApprove = "force_approve"
)

const (
	DecisionSourceUser   = "user"
	DecisionSourceBypass = "bypass"
)

const (
	TriggerTypeThreshold = "threshold"
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

const (
	KnowledgeStatusPending = "pending"
	KnowledgeStatusActive  = "active"
)

const (
	KnowledgeKindMemory     = "memory"
	KnowledgeKindPreference = "preference"
	KnowledgeKindSOP        = "sop"
	KnowledgeKindIncident   = "incident"
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
	ID          string      `json:"id"`
	HostID      string      `json:"host_id"`
	Title       string      `json:"title"`
	Summary     string      `json:"summary,omitempty"`
	Mode        SessionMode `json:"mode,omitempty"`
	Memory      MemoryState `json:"memory,omitempty"`
	TurnIDs     []string    `json:"turn_ids,omitempty"`
	RunIDs      []string    `json:"run_ids,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	LastInput   string      `json:"last_input,omitempty"`
	LastOutcome string      `json:"last_outcome,omitempty"`
}

type RuntimeSettings struct {
	MaxAgentSteps            int  `json:"max_agent_steps"`
	BypassApprovals          bool `json:"bypass_approvals"`
	ContextSoftLimitTokens   int  `json:"context_soft_limit_tokens"`
	CompressionTriggerTokens int  `json:"compression_trigger_tokens"`
	ResponseReserveTokens    int  `json:"response_reserve_tokens"`
	RecentFullTurns          int  `json:"recent_full_turns"`
	OlderUserLedgerEntries   int  `json:"older_user_ledger_entries"`
	HostProfileTTLMinutes    int  `json:"host_profile_ttl_minutes"`
	ToolResultMaxChars       int  `json:"tool_result_max_chars"`
	ToolResultHeadChars      int  `json:"tool_result_head_chars"`
	ToolResultTailChars      int  `json:"tool_result_tail_chars"`
	SOPRetrievalLimit        int  `json:"sop_retrieval_limit"`
}

type SessionMode struct {
	BypassApprovals bool `json:"bypass_approvals"`
}

type HostProfile struct {
	Hostname     string   `json:"hostname,omitempty"`
	Kernel       string   `json:"kernel,omitempty"`
	Distro       string   `json:"distro,omitempty"`
	Shell        string   `json:"shell,omitempty"`
	User         string   `json:"user,omitempty"`
	HomeDir      string   `json:"home_dir,omitempty"`
	WorkingDir   string   `json:"working_dir,omitempty"`
	PathPreview  string   `json:"path_preview,omitempty"`
	InitSystem   string   `json:"init_system,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Raw          string   `json:"raw,omitempty"`
	Stale        bool     `json:"stale,omitempty"`
}

type MemoryState struct {
	RollingSummary        string      `json:"rolling_summary,omitempty"`
	CompressedUntilTurnID string      `json:"compressed_until_turn_id,omitempty"`
	OlderUserLedger       []string    `json:"older_user_ledger,omitempty"`
	OpenThreads           []string    `json:"open_threads,omitempty"`
	HostProfile           HostProfile `json:"host_profile,omitempty"`
	LastCompactedAt       *time.Time  `json:"last_compacted_at,omitempty"`
	LastHostProfileAt     *time.Time  `json:"last_host_profile_at,omitempty"`
	ProfileStale          bool        `json:"profile_stale,omitempty"`
}

type ToolExecutionRecord struct {
	ToolName       string `json:"tool_name"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	ApprovalID     string `json:"approval_id,omitempty"`
	CommandPreview string `json:"command_preview,omitempty"`
	RawResult      string `json:"raw_result,omitempty"`
	ModelResult    string `json:"model_result,omitempty"`
	PolicyOverride bool   `json:"policy_override,omitempty"`
	Truncated      bool   `json:"truncated,omitempty"`
	RawChars       int    `json:"raw_chars,omitempty"`
	ModelChars     int    `json:"model_chars,omitempty"`
	OmittedChars   int    `json:"omitted_chars,omitempty"`
}

type PromptStats struct {
	EstimatedPromptTokensBeforeCompression int  `json:"estimated_prompt_tokens_before_compression,omitempty"`
	EstimatedPromptTokens                  int  `json:"estimated_prompt_tokens,omitempty"`
	CompressionTriggered                   bool `json:"compression_triggered,omitempty"`
	CompressedTurnCount                    int  `json:"compressed_turn_count,omitempty"`
	RecentFullTurnCount                    int  `json:"recent_full_turn_count,omitempty"`
	MessageCount                           int  `json:"message_count,omitempty"`
}

type Turn struct {
	ID               string                `json:"id"`
	SessionID        string                `json:"session_id"`
	HostID           string                `json:"host_id"`
	UserInput        string                `json:"user_input"`
	ContextSnapshot  ContextSnapshot       `json:"context_snapshot"`
	FinalExplanation string                `json:"final_explanation,omitempty"`
	Messages         []ChatMessage         `json:"messages,omitempty"`
	ToolResults      []ToolExecutionRecord `json:"tool_results,omitempty"`
	PromptStats      PromptStats           `json:"prompt_stats,omitempty"`
	RunID            string                `json:"run_id"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
}

type Run struct {
	ID                   string       `json:"id"`
	SessionID            string       `json:"session_id"`
	TurnID               string       `json:"turn_id"`
	HostID               string       `json:"host_id"`
	Status               string       `json:"status"`
	RequestedBy          string       `json:"requested_by,omitempty"`
	Mode                 SessionMode  `json:"mode,omitempty"`
	PendingApproval      string       `json:"pending_approval,omitempty"`
	PendingBatchID       string       `json:"pending_batch_id,omitempty"`
	PendingBatchTotal    int          `json:"pending_batch_total,omitempty"`
	PendingBatchResolved int          `json:"pending_batch_resolved,omitempty"`
	ToolHistory          []string     `json:"tool_history,omitempty"`
	PolicyHistory        []PolicyRule `json:"policy_history,omitempty"`
	FinalResponse        string       `json:"final_response,omitempty"`
	FailureMessage       string       `json:"failure_message,omitempty"`
	CreatedAt            time.Time    `json:"created_at"`
	UpdatedAt            time.Time    `json:"updated_at"`
	CompletedAt          *time.Time   `json:"completed_at,omitempty"`
}

type Approval struct {
	ID               string     `json:"id"`
	RunID            string     `json:"run_id"`
	BatchID          string     `json:"batch_id,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	BatchIndex       int        `json:"batch_index,omitempty"`
	ToolName         string     `json:"tool_name"`
	RuleID           string     `json:"rule_id,omitempty"`
	RuleSeverity     string     `json:"rule_severity,omitempty"`
	RuleCategory     string     `json:"rule_category,omitempty"`
	Reason           string     `json:"reason"`
	Scope            string     `json:"scope"`
	SaferAlternative string     `json:"safer_alternative,omitempty"`
	RequestedBy      string     `json:"requested_by"`
	PolicyDecision   string     `json:"policy_decision,omitempty"`
	Decision         string     `json:"decision,omitempty"`
	DecisionSource   string     `json:"decision_source,omitempty"`
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

type CapabilityView struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	EvidenceCount int        `json:"evidence_count"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
}

type GatewayHealth struct {
	Status           string           `json:"status"`
	NoSandbox        bool             `json:"no_sandbox"`
	PresetID         string           `json:"preset_id,omitempty"`
	PresetName       string           `json:"preset_name,omitempty"`
	BaseURL          string           `json:"base_url,omitempty"`
	Model            string           `json:"model"`
	EmbeddingModel   string           `json:"embedding_model,omitempty"`
	PolicySummary    string           `json:"policy_summary"`
	TotalHosts       int              `json:"total_hosts"`
	TotalSessions    int              `json:"total_sessions"`
	TotalRuns        int              `json:"total_runs"`
	ActiveRuns       int              `json:"active_runs"`
	PendingApprovals int              `json:"pending_approvals"`
	Capabilities     []CapabilityView `json:"capabilities,omitempty"`
}

type HostView struct {
	Host
	Status           string     `json:"status"`
	SessionCount     int        `json:"session_count"`
	TotalRuns        int        `json:"total_runs"`
	ActiveRuns       int        `json:"active_runs"`
	PendingApprovals int        `json:"pending_approvals"`
	LastRunStatus    string     `json:"last_run_status,omitempty"`
	LastRunAt        *time.Time `json:"last_run_at,omitempty"`
}

type SessionView struct {
	Session
	HostDisplayName  string     `json:"host_display_name,omitempty"`
	HostMode         string     `json:"host_mode,omitempty"`
	RunStatus        string     `json:"run_status,omitempty"`
	PendingApprovals int        `json:"pending_approvals"`
	TurnCount        int        `json:"turn_count"`
	Preview          string     `json:"preview,omitempty"`
	LastEventAt      *time.Time `json:"last_event_at,omitempty"`
}

type RunView struct {
	Run
	SessionTitle     string     `json:"session_title,omitempty"`
	SessionPreview   string     `json:"session_preview,omitempty"`
	HostDisplayName  string     `json:"host_display_name,omitempty"`
	PendingApprovals int        `json:"pending_approvals"`
	HasForceApprove  bool       `json:"has_force_approve,omitempty"`
	LatestAssistant  string     `json:"latest_assistant,omitempty"`
	LastEventAt      *time.Time `json:"last_event_at,omitempty"`
	LastEventType    string     `json:"last_event_type,omitempty"`
}

type ApprovalView struct {
	Approval
	SessionID       string `json:"session_id,omitempty"`
	SessionTitle    string `json:"session_title,omitempty"`
	HostID          string `json:"host_id,omitempty"`
	HostDisplayName string `json:"host_display_name,omitempty"`
	RunStatus       string `json:"run_status,omitempty"`
}

type AutomationRule struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Enabled           bool       `json:"enabled"`
	HostID            string     `json:"host_id"`
	TriggerType       string     `json:"trigger_type"`
	Metric            string     `json:"metric"`
	Operator          string     `json:"operator"`
	Threshold         float64    `json:"threshold"`
	WindowMinutes     int        `json:"window_minutes"`
	CooldownMinutes   int        `json:"cooldown_minutes"`
	PromptTemplate    string     `json:"prompt_template"`
	SessionStrategy   string     `json:"session_strategy"`
	BypassApprovals   bool       `json:"bypass_approvals"`
	AllowForceApprove bool       `json:"allow_force_approve"`
	SessionID         string     `json:"session_id,omitempty"`
	LastTriggeredAt   *time.Time `json:"last_triggered_at,omitempty"`
	LastRunID         string     `json:"last_run_id,omitempty"`
	LastStatus        string     `json:"last_status,omitempty"`
	LastObservedValue float64    `json:"last_observed_value,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type AutomationRuleView struct {
	AutomationRule
	HostDisplayName string `json:"host_display_name,omitempty"`
}

type AutomationSample struct {
	Metric     string    `json:"metric"`
	Value      float64   `json:"value"`
	CapturedAt time.Time `json:"captured_at"`
}

type AutomationTestResult struct {
	Rule             AutomationRule   `json:"rule"`
	Sample           AutomationSample `json:"sample"`
	ThresholdMatched bool             `json:"threshold_matched"`
	CooldownBlocked  bool             `json:"cooldown_blocked"`
	RunCreated       bool             `json:"run_created"`
	Run              *Run             `json:"run,omitempty"`
	Message          string           `json:"message"`
}

type AuditEntry struct {
	ID        string         `json:"id"`
	RunID     string         `json:"run_id"`
	Kind      string         `json:"kind"`
	Summary   string         `json:"summary"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type KnowledgeItem struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Status          string     `json:"status"`
	Scope           string     `json:"scope"`
	Title           string     `json:"title"`
	Body            string     `json:"body"`
	SourceRunID     string     `json:"source_run_id,omitempty"`
	SourceTurnID    string     `json:"source_turn_id,omitempty"`
	SourceEventID   string     `json:"source_event_id,omitempty"`
	SourceSOPID     string     `json:"source_sop_id,omitempty"`
	Confidence      float64    `json:"confidence,omitempty"`
	Embedding       []float64  `json:"embedding,omitempty"`
	EmbeddingModel  string     `json:"embedding_model,omitempty"`
	EmbeddingStatus string     `json:"embedding_status,omitempty"`
	EmbeddingError  string     `json:"embedding_error,omitempty"`
	Tags            []string   `json:"tags,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ApprovedAt      *time.Time `json:"approved_at,omitempty"`
	ApprovedBy      string     `json:"approved_by,omitempty"`
}

type OperatorProfile struct {
	ID                       string    `json:"id"`
	ApprovalStrictness       string    `json:"approval_strictness"`
	AllowBypassApprovals     bool      `json:"allow_bypass_approvals"`
	AllowForceApprove        bool      `json:"allow_force_approve"`
	AllowPlaintextSSHWarning bool      `json:"allow_plaintext_ssh_warning"`
	AllowAutomationBypass    bool      `json:"allow_automation_bypass"`
	PreferReadOnlyFirst      bool      `json:"prefer_read_only_first"`
	RemoteValidationRequired bool      `json:"remote_validation_required"`
	Notes                    []string  `json:"notes,omitempty"`
	UpdatedAt                time.Time `json:"updated_at"`
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
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	RiskCategory      string   `json:"risk_category"`
	Examples          []string `json:"examples"`
	Flow              []string `json:"flow,omitempty"`
	DecisionHints     []string `json:"decision_hints,omitempty"`
	SaferAlternatives []string `json:"safer_alternatives,omitempty"`
}

type ContextSnapshot struct {
	HostID             string          `json:"host_id"`
	HostDisplayName    string          `json:"host_display_name"`
	HostMode           string          `json:"host_mode"`
	SessionSummary     string          `json:"session_summary,omitempty"`
	HostProfileSummary string          `json:"host_profile_summary,omitempty"`
	RollingSummary     string          `json:"rolling_summary,omitempty"`
	OlderUserLedger    []string        `json:"older_user_ledger,omitempty"`
	OpenThreads        []string        `json:"open_threads,omitempty"`
	OperatorProfile    OperatorProfile `json:"operator_profile,omitempty"`
	PolicySummary      string          `json:"policy_summary"`
	SkillSummaries     []SkillSummary  `json:"skill_summaries,omitempty"`
	KnowledgeMatches   []KnowledgeItem `json:"knowledge_matches,omitempty"`
	BuiltinSummaries   []ToolSummary   `json:"builtin_summaries,omitempty"`
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
	RuleID           string `json:"rule_id,omitempty"`
	Category         string `json:"category,omitempty"`
	Severity         string `json:"severity,omitempty"`
	Decision         string `json:"decision"`
	Reason           string `json:"reason"`
	Scope            string `json:"scope"`
	SaferAlternative string `json:"safer_alternative,omitempty"`
	OverrideAllowed  bool   `json:"override_allowed,omitempty"`
}

type TurnHistoryItem struct {
	Turn            Turn       `json:"turn"`
	Run             Run        `json:"run"`
	Events          []Event    `json:"events"`
	Approvals       []Approval `json:"approvals,omitempty"`
	ToolEvents      []Event    `json:"tool_events,omitempty"`
	AssistantText   string     `json:"assistant_text,omitempty"`
	ConsoleOutput   string     `json:"console_output,omitempty"`
	LastEventAt     *time.Time `json:"last_event_at,omitempty"`
	WaitingApproval bool       `json:"waiting_approval,omitempty"`
}

type ApprovalBatch struct {
	ID          string     `json:"id"`
	RunID       string     `json:"run_id"`
	Approvals   []Approval `json:"approvals"`
	Total       int        `json:"total"`
	Resolved    int        `json:"resolved"`
	Waiting     bool       `json:"waiting"`
	Executing   bool       `json:"executing"`
	Completed   bool       `json:"completed"`
	HasOverride bool       `json:"has_override"`
}

type SessionDetail struct {
	Session          Session           `json:"session"`
	Host             Host              `json:"host"`
	Memory           MemoryState       `json:"memory,omitempty"`
	Turns            []TurnHistoryItem `json:"turns"`
	PendingApprovals []Approval        `json:"pending_approvals"`
	PendingBatches   []ApprovalBatch   `json:"pending_batches,omitempty"`
}

type GatewayPreset struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	BaseURL   string    `json:"base_url"`
	APIKey    string    `json:"api_key"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type GatewayConfig struct {
	CurrentPresetID string          `json:"current_preset_id"`
	Presets         []GatewayPreset `json:"presets"`
	RuntimeSettings RuntimeSettings `json:"runtime_settings,omitempty"`
	EmbeddingModel  string          `json:"embedding_model,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at,omitempty"`
}

type GatewayConfigView struct {
	CurrentPresetID string          `json:"current_preset_id"`
	CurrentPreset   *GatewayPreset  `json:"current_preset,omitempty"`
	Presets         []GatewayPreset `json:"presets"`
	RuntimeSettings RuntimeSettings `json:"runtime_settings,omitempty"`
	EmbeddingModel  string          `json:"embedding_model,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at,omitempty"`
}

type ConversationContext struct {
	Session          Session         `json:"session"`
	CurrentTurn      Turn            `json:"current_turn"`
	HistoricalTurns  []Turn          `json:"historical_turns,omitempty"`
	RuntimeSettings  RuntimeSettings `json:"runtime_settings"`
	OperatorProfile  OperatorProfile `json:"operator_profile,omitempty"`
	KnowledgeMatches []KnowledgeItem `json:"knowledge_matches,omitempty"`
}

type ExecutionResult struct {
	FinalResponse string                `json:"final_response"`
	ToolHistory   []string              `json:"tool_history,omitempty"`
	PolicyHistory []PolicyRule          `json:"policy_history,omitempty"`
	Messages      []ChatMessage         `json:"messages,omitempty"`
	ToolResults   []ToolExecutionRecord `json:"tool_results,omitempty"`
	PromptStats   PromptStats           `json:"prompt_stats,omitempty"`
	Memory        MemoryState           `json:"memory,omitempty"`
}

func DefaultRuntimeSettings() RuntimeSettings {
	return RuntimeSettings{
		MaxAgentSteps:            20,
		BypassApprovals:          false,
		ContextSoftLimitTokens:   20000,
		CompressionTriggerTokens: 16000,
		ResponseReserveTokens:    4000,
		RecentFullTurns:          2,
		OlderUserLedgerEntries:   6,
		HostProfileTTLMinutes:    30,
		ToolResultMaxChars:       6000,
		ToolResultHeadChars:      4000,
		ToolResultTailChars:      1200,
		SOPRetrievalLimit:        3,
	}
}

func DefaultOperatorProfile() OperatorProfile {
	return OperatorProfile{
		ID:                       "default",
		ApprovalStrictness:       "standard",
		AllowBypassApprovals:     false,
		AllowForceApprove:        true,
		AllowPlaintextSSHWarning: true,
		AllowAutomationBypass:    false,
		PreferReadOnlyFirst:      true,
		RemoteValidationRequired: true,
	}
}
