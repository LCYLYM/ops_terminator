package gateway

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	contextbuilder "osagentmvp/internal/context"
	"osagentmvp/internal/events"
	"osagentmvp/internal/models"
	"osagentmvp/internal/policy"
	"osagentmvp/internal/store"
)

type Runtime interface {
	Execute(context.Context, models.Run, models.Host, models.ConversationContext) (models.ExecutionResult, error)
}

type Service struct {
	store     store.Store
	hub       *events.Hub
	builder   *contextbuilder.Builder
	runtime   Runtime
	approvals ApprovalResolver
	logger    *log.Logger
	llmClient GatewayClient

	settingsMu    sync.RWMutex
	gatewayConfig models.GatewayConfig
}

type ApprovalResolver interface {
	Resolve(id, decision, actor string) (models.Approval, bool, error)
}

type GatewayClient interface {
	UpdateConfig(baseURL, apiKey, model string)
	SnapshotConfig() (baseURL, apiKey, model string)
}

type RunRequest struct {
	HostID      string `json:"host_id"`
	SessionID   string `json:"session_id,omitempty"`
	UserInput   string `json:"user_input"`
	RequestedBy string `json:"requested_by,omitempty"`
}

func NewService(store store.Store, hub *events.Hub, builder *contextbuilder.Builder, runtime Runtime, logger *log.Logger) *Service {
	return &Service{store: store, hub: hub, builder: builder, runtime: runtime, logger: logger}
}

func (s *Service) SetRuntime(runtime Runtime) {
	s.runtime = runtime
}

func (s *Service) SetApprovals(resolver ApprovalResolver) {
	s.approvals = resolver
}

func (s *Service) SetLLMClient(client GatewayClient) {
	s.llmClient = client
}

func (s *Service) SetGatewayConfig(config models.GatewayConfig) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.gatewayConfig = cloneGatewayConfig(config)
}

func (s *Service) EnsureBootstrapState() error {
	if _, found, err := s.store.GetHost("local"); err != nil {
		return err
	} else if found {
		return nil
	}
	now := time.Now().UTC()
	return s.store.SaveHost(models.Host{
		ID:          "local",
		DisplayName: "本机",
		Mode:        models.HostModeLocal,
		Tags:        []string{"bootstrap", "local"},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
}

func (s *Service) HealthSnapshot() (models.GatewayHealth, error) {
	state, err := s.loadDashboardState()
	if err != nil {
		return models.GatewayHealth{}, err
	}
	activePreset, err := s.activeGatewayPreset()
	if err != nil {
		return models.GatewayHealth{}, err
	}
	activeRuns := 0
	for _, run := range state.runs {
		if isActiveRunStatus(run.Status) {
			activeRuns++
		}
	}
	return models.GatewayHealth{
		Status:           "ok",
		NoSandbox:        true,
		PresetID:         activePreset.ID,
		PresetName:       activePreset.Name,
		BaseURL:          activePreset.BaseURL,
		Model:            activePreset.Model,
		PolicySummary:    policy.New().Summary(),
		TotalHosts:       len(state.hosts),
		TotalSessions:    len(state.sessions),
		TotalRuns:        len(state.runs),
		ActiveRuns:       activeRuns,
		PendingApprovals: state.pendingApprovalCount,
		Capabilities:     buildCapabilityViews(state),
	}, nil
}

func (s *Service) GatewayConfigView() models.GatewayConfigView {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return buildGatewayConfigView(s.gatewayConfig)
}

func (s *Service) UpdateGatewayConfig(next models.GatewayConfig) (models.GatewayConfigView, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	validated, activePreset, err := validateGatewayConfig(next, s.gatewayConfig)
	if err != nil {
		return models.GatewayConfigView{}, err
	}
	if err := s.store.SaveGatewayConfig(validated); err != nil {
		return models.GatewayConfigView{}, fmt.Errorf("save gateway config: %w", err)
	}
	s.gatewayConfig = cloneGatewayConfig(validated)
	if s.llmClient != nil {
		s.llmClient.UpdateConfig(activePreset.BaseURL, activePreset.APIKey, activePreset.Model)
	}
	return buildGatewayConfigView(validated), nil
}

func (s *Service) currentRuntimeSettings() models.RuntimeSettings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return normalizeRuntimeSettings(s.gatewayConfig.RuntimeSettings)
}

func (s *Service) ListHosts() ([]models.HostView, error) {
	state, err := s.loadDashboardState()
	if err != nil {
		return nil, err
	}
	items := make([]models.HostView, 0, len(state.hosts))
	for _, host := range state.hosts {
		view := models.HostView{
			Host:             host,
			Status:           "ready",
			SessionCount:     state.sessionCountByHost[host.ID],
			TotalRuns:        state.runCountByHost[host.ID],
			ActiveRuns:       state.activeRunCountByHost[host.ID],
			PendingApprovals: state.pendingApprovalCountByHost[host.ID],
			LastRunStatus:    state.lastRunStatusByHost[host.ID],
			LastRunAt:        cloneTimePtr(state.lastRunAtByHost[host.ID]),
		}
		if view.ActiveRuns > 0 {
			view.Status = "active"
		}
		items = append(items, view)
	}
	sort.Slice(items, func(i, j int) bool {
		left := items[i].LastRunAt
		right := items[j].LastRunAt
		switch {
		case left == nil && right == nil:
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		case left == nil:
			return false
		case right == nil:
			return true
		default:
			return left.After(*right)
		}
	})
	return items, nil
}

func (s *Service) ListRuns() ([]models.RunView, error) {
	state, err := s.loadDashboardState()
	if err != nil {
		return nil, err
	}
	items := make([]models.RunView, 0, len(state.runs))
	for _, run := range state.runs {
		session := state.sessionByID[run.SessionID]
		host := state.hostByID[run.HostID]
		view := models.RunView{
			Run:              run,
			SessionTitle:     session.Title,
			SessionPreview:   sessionPreview(session),
			HostDisplayName:  host.DisplayName,
			PendingApprovals: state.pendingApprovalCountByRun[run.ID],
			LatestAssistant:  firstNonEmpty(run.FinalResponse, run.FailureMessage),
			LastEventAt:      runLastActivity(run),
			LastEventType:    runEventType(run),
		}
		items = append(items, view)
	}
	sort.Slice(items, func(i, j int) bool {
		left := items[i].LastEventAt
		right := items[j].LastEventAt
		switch {
		case left == nil && right == nil:
			return items[i].CreatedAt.After(items[j].CreatedAt)
		case left == nil:
			return false
		case right == nil:
			return true
		default:
			return left.After(*right)
		}
	})
	return items, nil
}

func (s *Service) ListApprovals() ([]models.ApprovalView, error) {
	state, err := s.loadDashboardState()
	if err != nil {
		return nil, err
	}
	items := make([]models.ApprovalView, 0, len(state.approvals))
	for _, approval := range state.approvals {
		run := state.runByID[approval.RunID]
		session := state.sessionByID[run.SessionID]
		host := state.hostByID[run.HostID]
		items = append(items, models.ApprovalView{
			Approval:        approval,
			SessionID:       run.SessionID,
			SessionTitle:    session.Title,
			HostID:          run.HostID,
			HostDisplayName: host.DisplayName,
			RunStatus:       run.Status,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (s *Service) ListSessions() ([]models.SessionView, error) {
	state, err := s.loadDashboardState()
	if err != nil {
		return nil, err
	}
	items := make([]models.SessionView, 0, len(state.sessions))
	for _, session := range state.sessions {
		host := state.hostByID[session.HostID]
		latestRun := state.latestRunBySession[session.ID]
		view := models.SessionView{
			Session:          session,
			HostDisplayName:  host.DisplayName,
			HostMode:         host.Mode,
			RunStatus:        latestRun.Status,
			PendingApprovals: state.pendingApprovalCountBySession[session.ID],
			TurnCount:        len(session.TurnIDs),
			Preview:          sessionPreview(session),
			LastEventAt:      runLastActivity(latestRun),
		}
		items = append(items, view)
	}
	sort.Slice(items, func(i, j int) bool {
		left := items[i].LastEventAt
		right := items[j].LastEventAt
		switch {
		case left == nil && right == nil:
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		case left == nil:
			return false
		case right == nil:
			return true
		default:
			return left.After(*right)
		}
	})
	return items, nil
}

func (s *Service) UpsertHost(host models.Host) (models.Host, error) {
	host = normalizeHost(host)
	if err := validateHost(host); err != nil {
		return models.Host{}, err
	}
	now := time.Now().UTC()
	existing, found, err := s.store.GetHost(host.ID)
	if err != nil {
		return models.Host{}, err
	}
	if found {
		host.CreatedAt = existing.CreatedAt
	} else {
		host.CreatedAt = now
	}
	host.UpdatedAt = now
	if host.DisplayName == "" {
		host.DisplayName = host.ID
	}
	if host.Mode == "" {
		host.Mode = models.HostModeLocal
	}
	if host.Mode == models.HostModeSSH && host.Port == 0 {
		host.Port = 22
	}
	return host, s.store.SaveHost(host)
}

func (s *Service) CreateRun(ctx context.Context, request RunRequest) (models.Run, error) {
	host, found, err := s.store.GetHost(request.HostID)
	if err != nil {
		return models.Run{}, err
	}
	if !found {
		return models.Run{}, fmt.Errorf("host not found: %s", request.HostID)
	}
	session, err := s.ensureSession(host, request.SessionID, request.UserInput)
	if err != nil {
		return models.Run{}, err
	}
	settings := s.currentRuntimeSettings()
	session.Memory, err = s.builder.EnsureHostProfile(ctx, host, session.Memory, settings)
	if err != nil {
		return models.Run{}, err
	}
	snapshot := s.builder.Build(host, session, request.UserInput)
	now := time.Now().UTC()

	turn := models.Turn{
		ID:              models.NewID("turn"),
		SessionID:       session.ID,
		HostID:          host.ID,
		UserInput:       request.UserInput,
		ContextSnapshot: snapshot,
		Messages:        []models.ChatMessage{{Role: "user", Content: request.UserInput}},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	run := models.Run{
		ID:        models.NewID("run"),
		SessionID: session.ID,
		TurnID:    turn.ID,
		HostID:    host.ID,
		Status:    models.RunStatusCreated,
		CreatedAt: now,
		UpdatedAt: now,
	}
	turn.RunID = run.ID
	session.TurnIDs = append(session.TurnIDs, turn.ID)
	session.RunIDs = append(session.RunIDs, run.ID)
	session.LastInput = request.UserInput
	session.UpdatedAt = now

	if err := s.store.SaveSession(session); err != nil {
		return models.Run{}, err
	}
	if err := s.store.SaveTurn(turn); err != nil {
		return models.Run{}, err
	}
	if err := s.store.SaveRun(run); err != nil {
		return models.Run{}, err
	}
	_ = s.RecordEvent(models.Event{
		ID:        models.NewID("event"),
		RunID:     run.ID,
		Type:      "run.created",
		Message:   "run created",
		Payload:   map[string]any{"session_id": session.ID, "turn_id": turn.ID, "host_id": host.ID},
		Timestamp: now,
	})

	go s.processRun(context.Background(), run.ID)
	return run, nil
}

func (s *Service) GetRun(id string) (models.Run, bool, error) {
	return s.store.GetRun(id)
}

func (s *Service) ListRunEvents(runID string) ([]models.Event, error) {
	return s.store.ListEventsByRun(runID)
}

func (s *Service) SubscribeRun(runID string) (<-chan models.Event, func()) {
	return s.hub.Subscribe(runID)
}

func (s *Service) SubscribeAllEvents() (<-chan models.Event, func()) {
	return s.hub.SubscribeAll()
}

func (s *Service) ResolveApproval(id, decision, actor string) (models.Run, error) {
	if s.approvals == nil {
		return models.Run{}, fmt.Errorf("approval resolver is not configured")
	}
	approval, approved, err := s.approvals.Resolve(id, decision, actor)
	if err != nil {
		return models.Run{}, err
	}
	run, found, err := s.store.GetRun(approval.RunID)
	if err != nil {
		return models.Run{}, err
	}
	if !found {
		return models.Run{}, fmt.Errorf("run not found: %s", approval.RunID)
	}
	run.PendingApproval = ""
	if approved {
		run.Status = models.RunStatusRunningAgent
	} else {
		run.Status = models.RunStatusDenied
	}
	run.UpdatedAt = time.Now().UTC()
	if err := s.store.SaveRun(run); err != nil {
		return models.Run{}, err
	}
	_ = s.RecordEvent(models.Event{
		ID:        models.NewID("event"),
		RunID:     run.ID,
		Type:      "run.approval_resolved",
		Message:   decision,
		Payload:   map[string]any{"approval_id": id, "actor": actor},
		Timestamp: time.Now().UTC(),
	})
	return run, nil
}

func (s *Service) GetSessionDetail(id string) (models.SessionDetail, bool, error) {
	session, found, err := s.store.GetSession(id)
	if err != nil || !found {
		return models.SessionDetail{}, found, err
	}
	host, found, err := s.store.GetHost(session.HostID)
	if err != nil || !found {
		return models.SessionDetail{}, false, err
	}
	allTurns, err := s.store.ListTurns()
	if err != nil {
		return models.SessionDetail{}, false, err
	}
	allApprovals, err := s.store.ListApprovals()
	if err != nil {
		return models.SessionDetail{}, false, err
	}

	approvalsByRun := make(map[string][]models.Approval)
	pendingApprovals := make([]models.Approval, 0)
	for _, approval := range allApprovals {
		approvalsByRun[approval.RunID] = append(approvalsByRun[approval.RunID], approval)
		if approval.Decision == "" && contains(session.RunIDs, approval.RunID) {
			pendingApprovals = append(pendingApprovals, approval)
		}
	}

	items := make([]models.TurnHistoryItem, 0, len(session.TurnIDs))
	for _, turn := range allTurns {
		if turn.SessionID != session.ID {
			continue
		}
		run, _, err := s.store.GetRun(turn.RunID)
		if err != nil {
			return models.SessionDetail{}, false, err
		}
		runEvents, err := s.store.ListEventsByRun(turn.RunID)
		if err != nil {
			return models.SessionDetail{}, false, err
		}
		items = append(items, models.TurnHistoryItem{
			Turn:            turn,
			Run:             run,
			Events:          runEvents,
			Approvals:       approvalsByRun[turn.RunID],
			ToolEvents:      filterTraceEvents(runEvents),
			AssistantText:   deriveAssistantText(run, turn, runEvents),
			ConsoleOutput:   collectToolResultOutput(turn.ToolResults, runEvents),
			LastEventAt:     latestEventTimestamp(runEvents),
			WaitingApproval: hasPendingApproval(approvalsByRun[turn.RunID]),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Turn.CreatedAt.Before(items[j].Turn.CreatedAt)
	})
	sort.Slice(pendingApprovals, func(i, j int) bool {
		return pendingApprovals[i].CreatedAt.After(pendingApprovals[j].CreatedAt)
	})

	return models.SessionDetail{
		Session:          session,
		Host:             host,
		Memory:           session.Memory,
		Turns:            items,
		PendingApprovals: pendingApprovals,
	}, true, nil
}

func (s *Service) RecordEvent(event models.Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if err := s.store.AppendEvent(event); err != nil {
		return err
	}
	s.hub.Emit(event)
	return nil
}

func (s *Service) processRun(ctx context.Context, runID string) {
	run, found, err := s.store.GetRun(runID)
	if err != nil || !found {
		return
	}
	turn, _, err := s.store.GetTurn(run.TurnID)
	if err != nil {
		return
	}
	session, _, err := s.store.GetSession(run.SessionID)
	if err != nil {
		return
	}
	host, _, err := s.store.GetHost(run.HostID)
	if err != nil {
		return
	}

	run.Status = models.RunStatusRunningAgent
	run.UpdatedAt = time.Now().UTC()
	_ = s.store.SaveRun(run)

	allTurns, err := s.store.ListTurns()
	if err != nil {
		return
	}
	historyTurns := make([]models.Turn, 0, len(session.TurnIDs))
	for _, item := range allTurns {
		if item.SessionID != session.ID || item.ID == turn.ID {
			continue
		}
		historyTurns = append(historyTurns, item)
	}
	sort.Slice(historyTurns, func(i, j int) bool {
		return historyTurns[i].CreatedAt.Before(historyTurns[j].CreatedAt)
	})

	execResult, execErr := s.runtime.Execute(ctx, run, host, models.ConversationContext{
		Session:         session,
		CurrentTurn:     turn,
		HistoricalTurns: historyTurns,
		RuntimeSettings: s.currentRuntimeSettings(),
	})
	run.ToolHistory = execResult.ToolHistory
	run.PolicyHistory = execResult.PolicyHistory
	run.UpdatedAt = time.Now().UTC()

	if execErr != nil {
		now := time.Now().UTC()
		run.Status = models.RunStatusFailed
		run.FailureMessage = execErr.Error()
		run.CompletedAt = &now
		run.UpdatedAt = now
		_ = s.store.SaveRun(run)
		_ = s.RecordEvent(models.Event{
			ID:        models.NewID("event"),
			RunID:     run.ID,
			Type:      "run.failed",
			Message:   execErr.Error(),
			Timestamp: now,
		})
		return
	}

	now := time.Now().UTC()
	run.Status = models.RunStatusCompleted
	run.FinalResponse = execResult.FinalResponse
	run.CompletedAt = &now
	run.UpdatedAt = now
	turn.FinalExplanation = execResult.FinalResponse
	turn.Messages = execResult.Messages
	turn.ToolResults = execResult.ToolResults
	turn.PromptStats = execResult.PromptStats
	turn.UpdatedAt = now
	session.LastOutcome = execResult.FinalResponse
	session.Memory = execResult.Memory
	session.Summary = summarizeSession(session, turn, execResult.FinalResponse)
	session.UpdatedAt = now
	_ = s.store.SaveRun(run)
	_ = s.store.SaveTurn(turn)
	_ = s.store.SaveSession(session)
	_ = s.store.AppendAudit(models.AuditEntry{
		ID:        models.NewID("audit"),
		RunID:     run.ID,
		Kind:      "run_completed",
		Summary:   execResult.FinalResponse,
		CreatedAt: now,
	})
	_ = s.RecordEvent(models.Event{
		ID:        models.NewID("event"),
		RunID:     run.ID,
		Type:      "run.completed",
		Message:   execResult.FinalResponse,
		Timestamp: now,
	})
}

func (s *Service) ensureSession(host models.Host, sessionID, userInput string) (models.Session, error) {
	if sessionID != "" {
		session, found, err := s.store.GetSession(sessionID)
		if err != nil {
			return models.Session{}, err
		}
		if found {
			if session.HostID != "" && session.HostID != host.ID {
				return models.Session{}, fmt.Errorf("session %s belongs to host %s, not %s", session.ID, session.HostID, host.ID)
			}
			return session, nil
		}
	}
	now := time.Now().UTC()
	session := models.Session{
		ID:        models.NewID("session"),
		HostID:    host.ID,
		Title:     userInput,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return session, s.store.SaveSession(session)
}

func normalizeHost(host models.Host) models.Host {
	host.ID = strings.TrimSpace(host.ID)
	host.DisplayName = strings.TrimSpace(host.DisplayName)
	host.Mode = strings.TrimSpace(host.Mode)
	host.Address = strings.TrimSpace(host.Address)
	host.User = strings.TrimSpace(host.User)
	host.PasswordEnv = strings.TrimSpace(host.PasswordEnv)
	return host
}

func validateHost(host models.Host) error {
	if host.ID == "" {
		return fmt.Errorf("host id is required")
	}
	switch host.Mode {
	case "", models.HostModeLocal:
		return nil
	case models.HostModeSSH:
		if host.Address == "" {
			return fmt.Errorf("ssh host requires address")
		}
		if host.User == "" {
			return fmt.Errorf("ssh host requires user")
		}
		if host.PasswordEnv == "" {
			return fmt.Errorf("ssh host requires password_env")
		}
		if host.Port < 0 {
			return fmt.Errorf("ssh port must be positive")
		}
		return nil
	default:
		return fmt.Errorf("unsupported host mode: %s", host.Mode)
	}
}

func isValidEnvVarName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		switch {
		case r == '_':
			continue
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		default:
			return false
		}
	}
	return true
}

func summarizeSession(session models.Session, turn models.Turn, outcome string) string {
	if text := strings.TrimSpace(session.Memory.RollingSummary); text != "" {
		return text
	}
	return fmt.Sprintf("Last request: %s\nLast outcome: %s", turn.UserInput, outcome)
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

type dashboardState struct {
	hosts                         []models.Host
	sessions                      []models.Session
	runs                          []models.Run
	approvals                     []models.Approval
	hostByID                      map[string]models.Host
	sessionByID                   map[string]models.Session
	runByID                       map[string]models.Run
	sessionCountByHost            map[string]int
	runCountByHost                map[string]int
	activeRunCountByHost          map[string]int
	pendingApprovalCountByRun     map[string]int
	pendingApprovalCountByHost    map[string]int
	pendingApprovalCountBySession map[string]int
	pendingApprovalCount          int
	lastRunStatusByHost           map[string]string
	lastRunAtByHost               map[string]*time.Time
	latestRunBySession            map[string]models.Run
}

func (s *Service) loadDashboardState() (dashboardState, error) {
	hosts, err := s.store.ListHosts()
	if err != nil {
		return dashboardState{}, err
	}
	sessions, err := s.store.ListSessions()
	if err != nil {
		return dashboardState{}, err
	}
	runs, err := s.store.ListRuns()
	if err != nil {
		return dashboardState{}, err
	}
	approvals, err := s.store.ListApprovals()
	if err != nil {
		return dashboardState{}, err
	}
	state := dashboardState{
		hosts:                         hosts,
		sessions:                      sessions,
		runs:                          runs,
		approvals:                     approvals,
		hostByID:                      make(map[string]models.Host, len(hosts)),
		sessionByID:                   make(map[string]models.Session, len(sessions)),
		runByID:                       make(map[string]models.Run, len(runs)),
		sessionCountByHost:            make(map[string]int),
		runCountByHost:                make(map[string]int),
		activeRunCountByHost:          make(map[string]int),
		pendingApprovalCountByRun:     make(map[string]int),
		pendingApprovalCountByHost:    make(map[string]int),
		pendingApprovalCountBySession: make(map[string]int),
		lastRunStatusByHost:           make(map[string]string),
		lastRunAtByHost:               make(map[string]*time.Time),
		latestRunBySession:            make(map[string]models.Run),
	}
	for _, host := range hosts {
		state.hostByID[host.ID] = host
	}
	for _, session := range sessions {
		state.sessionByID[session.ID] = session
		state.sessionCountByHost[session.HostID]++
	}
	for _, run := range runs {
		state.runByID[run.ID] = run
		state.runCountByHost[run.HostID]++
		if isActiveRunStatus(run.Status) {
			state.activeRunCountByHost[run.HostID]++
		}
		if current, ok := state.latestRunBySession[run.SessionID]; !ok || run.UpdatedAt.After(current.UpdatedAt) {
			state.latestRunBySession[run.SessionID] = run
		}
		if last := state.lastRunAtByHost[run.HostID]; last == nil || run.UpdatedAt.After(*last) {
			ts := run.UpdatedAt
			state.lastRunAtByHost[run.HostID] = &ts
			state.lastRunStatusByHost[run.HostID] = run.Status
		}
	}
	for _, approval := range approvals {
		if approval.Decision != "" {
			continue
		}
		state.pendingApprovalCount++
		state.pendingApprovalCountByRun[approval.RunID]++
		run := state.runByID[approval.RunID]
		state.pendingApprovalCountByHost[run.HostID]++
		state.pendingApprovalCountBySession[run.SessionID]++
	}
	return state, nil
}

func buildCapabilityViews(state dashboardState) []models.CapabilityView {
	items := make([]models.CapabilityView, 0, 4)
	items = append(items, models.CapabilityView{
		ID:            "session-replay",
		Title:         "会话回放",
		Description:   fmt.Sprintf("当前持久化了 %d 个 sessions 和 %d 个 runs，可直接回放真实对话历史。", len(state.sessions), len(state.runs)),
		EvidenceCount: len(state.sessions) + len(state.runs),
		LastSeenAt:    latestSessionTimestamp(state.sessions),
	})
	modeSet := make(map[string]struct{})
	for _, host := range state.hosts {
		modeSet[host.Mode] = struct{}{}
	}
	items = append(items, models.CapabilityView{
		ID:            "host-connectivity",
		Title:         "主机接入",
		Description:   fmt.Sprintf("已登记 %d 台主机，接入模式覆盖 %s。", len(state.hosts), joinModes(modeSet)),
		EvidenceCount: len(state.hosts),
		LastSeenAt:    latestHostTimestamp(state.hosts),
	})
	items = append(items, models.CapabilityView{
		ID:            "approval-flow",
		Title:         "审批闭环",
		Description:   fmt.Sprintf("累计记录 %d 条审批，其中 %d 条仍待人工处理。", len(state.approvals), state.pendingApprovalCount),
		EvidenceCount: len(state.approvals),
		LastSeenAt:    latestApprovalTimestamp(state.approvals),
	})
	toolNames := observedToolNames(state.runs, state.approvals)
	items = append(items, models.CapabilityView{
		ID:            "tool-surface",
		Title:         "已观测工具面",
		Description:   fmt.Sprintf("真实运行中已出现 %d 种工具：%s。", len(toolNames), joinToolNames(toolNames)),
		EvidenceCount: len(toolNames),
		LastSeenAt:    latestRunTimestamp(state.runs),
	})
	return items
}

func filterTraceEvents(events []models.Event) []models.Event {
	result := make([]models.Event, 0, len(events))
	for _, event := range events {
		switch event.Type {
		case "run.created", "run.running_agent", "run.policy_checked", "run.tool_running", "run.tool_finished", "run.waiting_approval", "run.approval_resolved", "run.completed", "run.failed":
			result = append(result, event)
		}
	}
	return result
}

func deriveAssistantText(run models.Run, turn models.Turn, events []models.Event) string {
	for i := len(turn.Messages) - 1; i >= 0; i-- {
		message := turn.Messages[i]
		if message.Role == "assistant" && strings.TrimSpace(message.Content) != "" {
			return message.Content
		}
	}
	if text := firstNonEmpty(turn.FinalExplanation, run.FinalResponse, run.FailureMessage); text != "" {
		return text
	}
	var delta strings.Builder
	latestMessage := ""
	for _, event := range events {
		switch event.Type {
		case "run.message_delta":
			delta.WriteString(event.Message)
		case "run.assistant_message":
			if strings.TrimSpace(event.Message) != "" {
				latestMessage = event.Message
			}
		}
	}
	return firstNonEmpty(delta.String(), latestMessage)
}

func collectConsoleOutput(events []models.Event) string {
	return collectToolResultOutput(nil, events)
}

func collectToolResultOutput(results []models.ToolExecutionRecord, events []models.Event) string {
	if len(results) > 0 {
		parts := make([]string, 0, len(results))
		for _, result := range results {
			text := strings.TrimSpace(firstNonEmpty(result.RawResult, result.ModelResult))
			if text == "" {
				continue
			}
			parts = append(parts, text)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n\n")
		}
	}
	var builder strings.Builder
	for _, event := range events {
		switch event.Type {
		case "run.stdout":
			builder.WriteString(event.Message)
		case "run.stderr":
			builder.WriteString("[stderr] ")
			builder.WriteString(event.Message)
		}
	}
	return strings.TrimSpace(builder.String())
}

func latestEventTimestamp(events []models.Event) *time.Time {
	var latest *time.Time
	for _, event := range events {
		if latest == nil || event.Timestamp.After(*latest) {
			ts := event.Timestamp
			latest = &ts
		}
	}
	return latest
}

func hasPendingApproval(items []models.Approval) bool {
	for _, item := range items {
		if item.Decision == "" {
			return true
		}
	}
	return false
}

func runLastActivity(run models.Run) *time.Time {
	if run.CompletedAt != nil {
		return cloneTimePtr(run.CompletedAt)
	}
	if run.UpdatedAt.IsZero() {
		return nil
	}
	ts := run.UpdatedAt
	return &ts
}

func runEventType(run models.Run) string {
	switch run.Status {
	case models.RunStatusCompleted:
		return "run.completed"
	case models.RunStatusFailed:
		return "run.failed"
	case models.RunStatusWaitingApproval:
		return "run.waiting_approval"
	default:
		return "run.updated"
	}
}

func sessionPreview(session models.Session) string {
	return firstNonEmpty(session.LastOutcome, session.Memory.RollingSummary, session.Summary, session.LastInput)
}

func isActiveRunStatus(status string) bool {
	switch status {
	case models.RunStatusCreated, models.RunStatusRunningAgent, models.RunStatusToolRunning, models.RunStatusWaitingApproval:
		return true
	default:
		return false
	}
}

func observedToolNames(runs []models.Run, approvals []models.Approval) []string {
	counts := make(map[string]int)
	for _, run := range runs {
		for _, rule := range run.PolicyHistory {
			if name := strings.TrimSpace(rule.Scope); name != "" && !strings.Contains(name, " ") {
				counts[name]++
			}
		}
		for _, entry := range run.ToolHistory {
			name := strings.TrimSpace(strings.SplitN(entry, ":", 2)[0])
			if name != "" {
				counts[name]++
			}
		}
	}
	for _, approval := range approvals {
		if approval.ToolName != "" {
			counts[approval.ToolName]++
		}
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func joinModes(items map[string]struct{}) string {
	if len(items) == 0 {
		return "暂无主机"
	}
	names := make([]string, 0, len(items))
	for name := range items {
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "暂无主机"
	}
	return strings.Join(names, " / ")
}

func joinToolNames(items []string) string {
	if len(items) == 0 {
		return "暂无工具运行证据"
	}
	limit := items
	if len(limit) > 4 {
		limit = limit[:4]
	}
	return strings.Join(limit, " / ")
}

func latestHostTimestamp(items []models.Host) *time.Time {
	var latest *time.Time
	for _, item := range items {
		if latest == nil || item.UpdatedAt.After(*latest) {
			ts := item.UpdatedAt
			latest = &ts
		}
	}
	return latest
}

func latestSessionTimestamp(items []models.Session) *time.Time {
	var latest *time.Time
	for _, item := range items {
		if latest == nil || item.UpdatedAt.After(*latest) {
			ts := item.UpdatedAt
			latest = &ts
		}
	}
	return latest
}

func latestRunTimestamp(items []models.Run) *time.Time {
	var latest *time.Time
	for _, item := range items {
		activity := runLastActivity(item)
		if activity == nil {
			continue
		}
		if latest == nil || activity.After(*latest) {
			ts := *activity
			latest = &ts
		}
	}
	return latest
}

func latestApprovalTimestamp(items []models.Approval) *time.Time {
	var latest *time.Time
	for _, item := range items {
		ts := item.CreatedAt
		if item.ResolvedAt != nil {
			ts = *item.ResolvedAt
		}
		if latest == nil || ts.After(*latest) {
			copyTS := ts
			latest = &copyTS
		}
	}
	return latest
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Service) activeGatewayPreset() (models.GatewayPreset, error) {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return activeGatewayPreset(s.gatewayConfig)
}

func activeGatewayPreset(config models.GatewayConfig) (models.GatewayPreset, error) {
	currentID := strings.TrimSpace(config.CurrentPresetID)
	if currentID == "" {
		return models.GatewayPreset{}, fmt.Errorf("gateway config current preset is empty")
	}
	for _, preset := range config.Presets {
		if preset.ID == currentID {
			return preset, nil
		}
	}
	return models.GatewayPreset{}, fmt.Errorf("gateway preset %q not found", currentID)
}

func buildGatewayConfigView(config models.GatewayConfig) models.GatewayConfigView {
	view := models.GatewayConfigView{
		CurrentPresetID: config.CurrentPresetID,
		Presets:         append([]models.GatewayPreset(nil), config.Presets...),
		RuntimeSettings: normalizeRuntimeSettings(config.RuntimeSettings),
		UpdatedAt:       config.UpdatedAt,
	}
	if active, err := activeGatewayPreset(config); err == nil {
		copyPreset := active
		view.CurrentPreset = &copyPreset
	}
	return view
}

func cloneGatewayConfig(config models.GatewayConfig) models.GatewayConfig {
	cloned := models.GatewayConfig{
		CurrentPresetID: config.CurrentPresetID,
		RuntimeSettings: normalizeRuntimeSettings(config.RuntimeSettings),
		UpdatedAt:       config.UpdatedAt,
		Presets:         make([]models.GatewayPreset, len(config.Presets)),
	}
	copy(cloned.Presets, config.Presets)
	return cloned
}

func validateGatewayConfig(next, previous models.GatewayConfig) (models.GatewayConfig, models.GatewayPreset, error) {
	now := time.Now().UTC()
	result := models.GatewayConfig{
		CurrentPresetID: strings.TrimSpace(next.CurrentPresetID),
		RuntimeSettings: normalizeRuntimeSettings(next.RuntimeSettings),
		Presets:         make([]models.GatewayPreset, 0, len(next.Presets)),
		UpdatedAt:       now,
	}
	if len(next.Presets) == 0 {
		return models.GatewayConfig{}, models.GatewayPreset{}, fmt.Errorf("at least one gateway preset is required")
	}

	previousByID := make(map[string]models.GatewayPreset, len(previous.Presets))
	for _, preset := range previous.Presets {
		previousByID[preset.ID] = preset
	}

	seen := make(map[string]struct{}, len(next.Presets))
	for _, preset := range next.Presets {
		preset.ID = strings.TrimSpace(preset.ID)
		preset.Name = strings.TrimSpace(preset.Name)
		preset.BaseURL = strings.TrimRight(strings.TrimSpace(preset.BaseURL), "/")
		preset.APIKey = strings.TrimSpace(preset.APIKey)
		preset.Model = strings.TrimSpace(preset.Model)
		if preset.ID == "" {
			preset.ID = models.NewID("gateway")
		}
		if _, exists := seen[preset.ID]; exists {
			return models.GatewayConfig{}, models.GatewayPreset{}, fmt.Errorf("duplicate gateway preset id: %s", preset.ID)
		}
		seen[preset.ID] = struct{}{}
		if preset.Name == "" {
			return models.GatewayConfig{}, models.GatewayPreset{}, fmt.Errorf("gateway preset name is required")
		}
		if preset.BaseURL == "" {
			return models.GatewayConfig{}, models.GatewayPreset{}, fmt.Errorf("gateway preset %q base_url is required", preset.Name)
		}
		if preset.APIKey == "" {
			return models.GatewayConfig{}, models.GatewayPreset{}, fmt.Errorf("gateway preset %q api_key is required", preset.Name)
		}
		if preset.Model == "" {
			return models.GatewayConfig{}, models.GatewayPreset{}, fmt.Errorf("gateway preset %q model is required", preset.Name)
		}
		if previousPreset, ok := previousByID[preset.ID]; ok {
			preset.CreatedAt = previousPreset.CreatedAt
		}
		if preset.CreatedAt.IsZero() {
			preset.CreatedAt = now
		}
		preset.UpdatedAt = now
		result.Presets = append(result.Presets, preset)
	}

	if result.CurrentPresetID == "" {
		result.CurrentPresetID = result.Presets[0].ID
	}
	activePreset, err := activeGatewayPreset(result)
	if err != nil {
		return models.GatewayConfig{}, models.GatewayPreset{}, err
	}
	return result, activePreset, nil
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
