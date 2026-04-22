package approval

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"osagentmvp/internal/models"
	"osagentmvp/internal/store"
)

type EventRecorder interface {
	RecordEvent(models.Event) error
}

type Manager struct {
	store   store.Store
	events  EventRecorder
	mu      sync.Mutex
	pending map[string]chan bool
}

func NewManager(store store.Store, events EventRecorder) *Manager {
	return &Manager{
		store:   store,
		events:  events,
		pending: make(map[string]chan bool),
	}
}

func (m *Manager) Wait(ctx context.Context, runID string, preview models.ActionPreview, rule models.PolicyRule) (models.Approval, bool, error) {
	now := time.Now().UTC()
	approval := models.Approval{
		ID:               models.NewID("approval"),
		RunID:            runID,
		ToolName:         preview.ToolName,
		Reason:           rule.Reason,
		Scope:            firstNonEmpty(preview.CommandPreview, preview.ToolName),
		SaferAlternative: rule.SaferAlternative,
		RequestedBy:      "gateway",
		CreatedAt:        now,
	}
	if err := m.store.SaveApproval(approval); err != nil {
		return models.Approval{}, false, err
	}
	run, found, err := m.store.GetRun(runID)
	if err == nil && found {
		run.Status = models.RunStatusWaitingApproval
		run.PendingApproval = approval.ID
		run.UpdatedAt = now
		_ = m.store.SaveRun(run)
	}
	_ = m.events.RecordEvent(models.Event{
		ID:        models.NewID("event"),
		RunID:     runID,
		Type:      "run.waiting_approval",
		Message:   "操作触发审批，等待人工确认。",
		Payload:   map[string]any{"approval_id": approval.ID, "tool_name": preview.ToolName, "scope": approval.Scope},
		Timestamp: now,
	})

	replyCh := make(chan bool, 1)
	m.mu.Lock()
	m.pending[approval.ID] = replyCh
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.pending, approval.ID)
		m.mu.Unlock()
	}()

	select {
	case approved := <-replyCh:
		return approval, approved, nil
	case <-ctx.Done():
		return approval, false, ctx.Err()
	}
}

func (m *Manager) Resolve(id, decision, actor string) (models.Approval, bool, error) {
	approval, found, err := m.store.GetApproval(id)
	if err != nil {
		return models.Approval{}, false, err
	}
	if !found {
		return models.Approval{}, false, fmt.Errorf("approval not found: %s", id)
	}
	now := time.Now().UTC()
	approval.Decision = decision
	approval.ResolvedBy = actor
	approval.ResolvedAt = &now
	if err := m.store.SaveApproval(approval); err != nil {
		return models.Approval{}, false, err
	}

	approved := strings.EqualFold(decision, "approve") || strings.EqualFold(decision, "approved") || strings.EqualFold(decision, "allow")
	run, found, err := m.store.GetRun(approval.RunID)
	if err == nil && found {
		run.PendingApproval = ""
		run.UpdatedAt = now
		if approved {
			run.Status = models.RunStatusRunningAgent
		} else {
			run.Status = models.RunStatusDenied
			run.CompletedAt = &now
		}
		_ = m.store.SaveRun(run)
	}
	m.mu.Lock()
	replyCh := m.pending[id]
	m.mu.Unlock()
	if replyCh != nil {
		replyCh <- approved
	}
	return approval, approved, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
