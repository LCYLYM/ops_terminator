package approval

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"osagentmvp/internal/models"
	"osagentmvp/internal/store"
)

type EventRecorder interface {
	RecordEvent(models.Event) error
}

type BatchRequest struct {
	ToolCall models.ToolCall
	Preview  models.ActionPreview
	Rule     models.PolicyRule
}

type pendingBatch struct {
	runID    string
	expected int
	done     chan struct{}
	closed   bool
}

type Manager struct {
	store   store.Store
	events  EventRecorder
	mu      sync.Mutex
	batches map[string]*pendingBatch
}

func NewManager(store store.Store, events EventRecorder) *Manager {
	return &Manager{
		store:   store,
		events:  events,
		batches: make(map[string]*pendingBatch),
	}
}

func (m *Manager) WaitBatch(ctx context.Context, runID string, items []BatchRequest) ([]models.Approval, error) {
	if len(items) == 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	batchID := models.NewID("approval_batch")
	approvals := make([]models.Approval, 0, len(items))
	for index, item := range items {
		approval := models.Approval{
			ID:               models.NewID("approval"),
			RunID:            runID,
			BatchID:          batchID,
			ToolCallID:       item.ToolCall.ID,
			BatchIndex:       index,
			ToolName:         item.Preview.ToolName,
			RuleID:           item.Rule.RuleID,
			RuleSeverity:     item.Rule.Severity,
			RuleCategory:     item.Rule.Category,
			Reason:           item.Rule.Reason,
			Scope:            firstNonEmpty(item.Preview.CommandPreview, item.Rule.Scope, item.Preview.ToolName),
			SaferAlternative: item.Rule.SaferAlternative,
			RequestedBy:      "gateway",
			PolicyDecision:   item.Rule.Decision,
			CreatedAt:        now,
		}
		if err := m.store.SaveApproval(approval); err != nil {
			return nil, err
		}
		if item.Rule.Decision == models.PolicyDecisionDeny {
			_ = m.events.RecordEvent(models.Event{
				ID:        models.NewID("event"),
				RunID:     runID,
				Type:      "run.policy_override_requested",
				Message:   item.Rule.Reason,
				Payload:   map[string]any{"approval_id": approval.ID, "tool_name": approval.ToolName, "rule_id": item.Rule.RuleID, "severity": item.Rule.Severity},
				Timestamp: now,
			})
		}
		approvals = append(approvals, approval)
	}

	run, found, err := m.store.GetRun(runID)
	if err == nil && found {
		run.Status = models.RunStatusWaitingApproval
		run.PendingApproval = approvals[0].ID
		run.PendingBatchID = batchID
		run.PendingBatchTotal = len(approvals)
		run.PendingBatchResolved = 0
		run.UpdatedAt = now
		_ = m.store.SaveRun(run)
	}

	_ = m.events.RecordEvent(models.Event{
		ID:      models.NewID("event"),
		RunID:   runID,
		Type:    "run.waiting_approval",
		Message: fmt.Sprintf("审批批次 %s 已创建，等待人工处理。", batchID),
		Payload: map[string]any{
			"batch_id":      batchID,
			"approval_ids":  approvalIDs(approvals),
			"pending_count": len(approvals),
		},
		Timestamp: now,
	})

	batch := &pendingBatch{
		runID:    runID,
		expected: len(approvals),
		done:     make(chan struct{}),
	}

	m.mu.Lock()
	m.batches[batchID] = batch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.batches, batchID)
		m.mu.Unlock()
	}()

	select {
	case <-batch.done:
		return m.listBatchApprovals(batchID)
	case <-ctx.Done():
		return nil, ctx.Err()
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
	if approval.Decision != "" {
		return approval, isApprovedDecision(approval.Decision), nil
	}

	normalizedDecision, err := normalizeDecision(decision, approval.PolicyDecision)
	if err != nil {
		return models.Approval{}, false, err
	}

	now := time.Now().UTC()
	approval.Decision = normalizedDecision
	approval.DecisionSource = models.DecisionSourceUser
	approval.ResolvedBy = actor
	approval.ResolvedAt = &now
	if err := m.store.SaveApproval(approval); err != nil {
		return models.Approval{}, false, err
	}

	allApprovals, err := m.listBatchApprovals(approval.BatchID)
	if err != nil {
		return models.Approval{}, false, err
	}

	run, found, err := m.store.GetRun(approval.RunID)
	if err == nil && found {
		resolvedCount := 0
		pendingID := ""
		for _, item := range allApprovals {
			if item.Decision != "" {
				resolvedCount++
				continue
			}
			if pendingID == "" {
				pendingID = item.ID
			}
		}
		run.PendingApproval = pendingID
		run.PendingBatchID = approval.BatchID
		run.PendingBatchTotal = len(allApprovals)
		run.PendingBatchResolved = resolvedCount
		run.UpdatedAt = now
		if resolvedCount >= len(allApprovals) {
			run.Status = models.RunStatusRunningAgent
		} else {
			run.Status = models.RunStatusWaitingApproval
		}
		_ = m.store.SaveRun(run)
	}

	if strings.EqualFold(normalizedDecision, models.ApprovalDecisionForceApprove) {
		_ = m.events.RecordEvent(models.Event{
			ID:        models.NewID("event"),
			RunID:     approval.RunID,
			Type:      "run.policy_override_resolved",
			Message:   "policy deny overridden by force approve",
			Payload:   map[string]any{"approval_id": approval.ID, "tool_name": approval.ToolName, "actor": actor},
			Timestamp: now,
		})
	}

	m.markBatchIfComplete(approval.BatchID, allApprovals)
	return approval, isApprovedDecision(normalizedDecision), nil
}

func (m *Manager) markBatchIfComplete(batchID string, approvals []models.Approval) {
	allResolved := len(approvals) > 0
	for _, item := range approvals {
		if item.Decision == "" {
			allResolved = false
			break
		}
	}
	if !allResolved {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	batch := m.batches[batchID]
	if batch == nil || batch.closed {
		return
	}
	close(batch.done)
	batch.closed = true
}

func (m *Manager) listBatchApprovals(batchID string) ([]models.Approval, error) {
	items, err := m.store.ListApprovals()
	if err != nil {
		return nil, err
	}
	result := make([]models.Approval, 0, len(items))
	for _, item := range items {
		if item.BatchID == batchID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].BatchIndex < result[j].BatchIndex
	})
	return result, nil
}

func normalizeDecision(decision, policyDecision string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case models.ApprovalDecisionApprove:
		if policyDecision == models.PolicyDecisionDeny {
			return "", fmt.Errorf("policy denied tool requires force_approve")
		}
		return models.ApprovalDecisionApprove, nil
	case models.ApprovalDecisionReject:
		return models.ApprovalDecisionReject, nil
	case models.ApprovalDecisionForceApprove:
		if policyDecision != models.PolicyDecisionDeny {
			return models.ApprovalDecisionApprove, nil
		}
		return models.ApprovalDecisionForceApprove, nil
	default:
		return "", fmt.Errorf("unsupported approval decision: %s", decision)
	}
}

func isApprovedDecision(decision string) bool {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case models.ApprovalDecisionApprove, models.ApprovalDecisionForceApprove:
		return true
	default:
		return false
	}
}

func approvalIDs(items []models.Approval) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
