package gateway

import (
	"context"
	"fmt"
	"log"
	"time"

	contextbuilder "osagentmvp/internal/context"
	"osagentmvp/internal/events"
	"osagentmvp/internal/models"
	"osagentmvp/internal/store"
)

type Runtime interface {
	Execute(context.Context, models.Run, models.Host, models.ContextSnapshot, string) (string, []string, []models.PolicyRule, error)
}

type Service struct {
	store     store.Store
	hub       *events.Hub
	builder   *contextbuilder.Builder
	runtime   Runtime
	approvals ApprovalResolver
	logger    *log.Logger
}

type ApprovalResolver interface {
	Resolve(id, decision, actor string) (models.Approval, bool, error)
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

func (s *Service) ListHosts() ([]models.Host, error)         { return s.store.ListHosts() }
func (s *Service) ListRuns() ([]models.Run, error)           { return s.store.ListRuns() }
func (s *Service) ListApprovals() ([]models.Approval, error) { return s.store.ListApprovals() }

func (s *Service) UpsertHost(host models.Host) (models.Host, error) {
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
	snapshot := s.builder.Build(host, session, request.UserInput)
	now := time.Now().UTC()

	turn := models.Turn{
		ID:              models.NewID("turn"),
		SessionID:       session.ID,
		HostID:          host.ID,
		UserInput:       request.UserInput,
		ContextSnapshot: snapshot,
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

func (s *Service) ResolveApproval(id, decision, actor string) (models.Run, error) {
	if s.approvals == nil {
		return models.Run{}, fmt.Errorf("approval resolver is not configured")
	}
	approval, _, err := s.approvals.Resolve(id, decision, actor)
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
	run.Status = models.RunStatusRunningAgent
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

	finalText, toolHistory, policyHistory, execErr := s.runtime.Execute(ctx, run, host, turn.ContextSnapshot, turn.UserInput)
	run.ToolHistory = toolHistory
	run.PolicyHistory = policyHistory
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
	run.FinalResponse = finalText
	run.CompletedAt = &now
	run.UpdatedAt = now
	turn.FinalExplanation = finalText
	turn.UpdatedAt = now
	session.LastOutcome = finalText
	session.Summary = summarizeSession(session, turn.UserInput, finalText)
	session.UpdatedAt = now
	_ = s.store.SaveRun(run)
	_ = s.store.SaveTurn(turn)
	_ = s.store.SaveSession(session)
	_ = s.store.AppendAudit(models.AuditEntry{
		ID:        models.NewID("audit"),
		RunID:     run.ID,
		Kind:      "run_completed",
		Summary:   finalText,
		CreatedAt: now,
	})
	_ = s.RecordEvent(models.Event{
		ID:        models.NewID("event"),
		RunID:     run.ID,
		Type:      "run.completed",
		Message:   finalText,
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

func summarizeSession(session models.Session, input, outcome string) string {
	return fmt.Sprintf("Last request: %s\nLast outcome: %s", input, outcome)
}
