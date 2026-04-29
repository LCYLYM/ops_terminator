package gateway

import (
	"context"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"osagentmvp/internal/builtin"
	contextbuilder "osagentmvp/internal/context"
	"osagentmvp/internal/events"
	"osagentmvp/internal/models"
	"osagentmvp/internal/policy"
	"osagentmvp/internal/runner"
	"osagentmvp/internal/store"
)

type gatewayFakeRuntime struct{}

func (gatewayFakeRuntime) Execute(context.Context, models.Run, models.Host, models.ConversationContext) (models.ExecutionResult, error) {
	return models.ExecutionResult{FinalResponse: "automation test completed"}, nil
}

func TestEnsureSessionRejectsCrossHostReuse(t *testing.T) {
	storeImpl, err := store.NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	service := NewService(storeImpl, events.NewHub(), nil, nil, log.New(io.Discard, "", 0))

	existing := models.Session{
		ID:        "session-local",
		HostID:    "local",
		Title:     "local session",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := storeImpl.SaveSession(existing); err != nil {
		t.Fatalf("save session: %v", err)
	}

	_, err = service.ensureSession(models.Host{ID: "ssh-1", Mode: models.HostModeSSH}, existing.ID, "check", models.SessionMode{}, nil)
	if err == nil {
		t.Fatal("expected host mismatch error")
	}
	if !strings.Contains(err.Error(), "belongs to host local") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpsertHostAllowsLiteralPassword(t *testing.T) {
	storeImpl, err := store.NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	service := NewService(storeImpl, events.NewHub(), nil, nil, log.New(io.Discard, "", 0))

	host, err := service.UpsertHost(models.Host{
		ID:          "ssh-1",
		Mode:        models.HostModeSSH,
		Address:     "203.0.113.10",
		User:        "root",
		PasswordEnv: "plain-text-test-password",
	})
	if err != nil {
		t.Fatalf("upsert host: %v", err)
	}
	if host.PasswordEnv != "plain-text-test-password" {
		t.Fatalf("unexpected password field: %q", host.PasswordEnv)
	}
}

func TestValidateGatewayConfigPreservesBypassApprovals(t *testing.T) {
	next := models.GatewayConfig{
		CurrentPresetID: "default",
		RuntimeSettings: models.RuntimeSettings{
			MaxAgentSteps:   4,
			BypassApprovals: true,
		},
		Presets: []models.GatewayPreset{
			{
				ID:      "default",
				Name:    "Default",
				BaseURL: "https://api.example.com/",
				APIKey:  "sk-test",
				Model:   "test-model",
			},
		},
	}

	validated, _, err := validateGatewayConfig(next, models.GatewayConfig{})
	if err != nil {
		t.Fatalf("validate gateway config: %v", err)
	}
	if !validated.RuntimeSettings.BypassApprovals {
		t.Fatal("expected bypass approvals to be preserved")
	}
	if validated.RuntimeSettings.MaxAgentSteps != 4 {
		t.Fatalf("unexpected max agent steps: %d", validated.RuntimeSettings.MaxAgentSteps)
	}
	if validated.RuntimeSettings.ContextSoftLimitTokens != models.DefaultRuntimeSettings().ContextSoftLimitTokens {
		t.Fatalf("expected runtime defaults to be filled, got %+v", validated.RuntimeSettings)
	}
	if validated.EmbeddingModel != "text-embedding-3-small" {
		t.Fatalf("expected embedding model default, got %q", validated.EmbeddingModel)
	}
}

func TestGatewayConfigViewMasksAPIKeyAndPreservesBlankUpdate(t *testing.T) {
	now := time.Now().UTC()
	previous := models.GatewayConfig{
		CurrentPresetID: "default",
		RuntimeSettings: models.DefaultRuntimeSettings(),
		Presets: []models.GatewayPreset{{
			ID:        "default",
			Name:      "Default",
			BaseURL:   "https://api.example.com",
			APIKey:    "sk-secret",
			Model:     "test-model",
			CreatedAt: now,
			UpdatedAt: now,
		}},
	}
	view := buildGatewayConfigView(previous)
	if view.Presets[0].APIKey != "" || !view.Presets[0].APIKeyConfigured {
		t.Fatalf("expected masked api key in view: %+v", view.Presets[0])
	}

	next := previous
	next.Presets[0].APIKey = ""
	validated, active, err := validateGatewayConfig(next, previous)
	if err != nil {
		t.Fatalf("validate gateway config: %v", err)
	}
	if validated.Presets[0].APIKey != "sk-secret" || active.APIKey != "sk-secret" {
		t.Fatalf("expected blank update to preserve previous key: %+v active=%+v", validated.Presets[0], active)
	}
}

func TestOperatorProfileDefaultsAndAudit(t *testing.T) {
	storeImpl, err := store.NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	service := NewService(storeImpl, events.NewHub(), nil, nil, log.New(io.Discard, "", 0))

	profile, err := service.OperatorProfile()
	if err != nil {
		t.Fatalf("operator profile: %v", err)
	}
	if !profile.PreferReadOnlyFirst || !profile.RemoteValidationRequired {
		t.Fatalf("unexpected defaults: %+v", profile)
	}
	profile.ApprovalStrictness = "strict"
	updated, err := service.UpdateOperatorProfile(profile, "test")
	if err != nil {
		t.Fatalf("update operator profile: %v", err)
	}
	if updated.ApprovalStrictness != "strict" {
		t.Fatalf("unexpected profile: %+v", updated)
	}
}

func TestPolicyConfigUpdateProtectsDenyRules(t *testing.T) {
	storeImpl, err := store.NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	policyEngine := policy.New()
	service := NewService(storeImpl, events.NewHub(), nil, nil, log.New(io.Discard, "", 0))
	service.SetPolicyEngine(policyEngine)

	config, err := service.PolicyConfig()
	if err != nil {
		t.Fatalf("policy config: %v", err)
	}
	if len(config.Rules) == 0 {
		t.Fatal("expected default policy rules")
	}
	for i := range config.Rules {
		if config.Rules[i].ID == "mutating_shell_ask" {
			config.Rules[i].Severity = "critical"
			config.Rules[i].Reason = "custom approval reason"
		}
	}
	updated, err := service.UpdatePolicyConfig(config, "test")
	if err != nil {
		t.Fatalf("update policy config: %v", err)
	}
	found := false
	for _, rule := range updated.Rules {
		if rule.ID == "mutating_shell_ask" {
			found = true
			if rule.Severity != "critical" || rule.Reason != "custom approval reason" {
				t.Fatalf("unexpected updated rule: %+v", rule)
			}
		}
	}
	if !found {
		t.Fatal("expected mutating_shell_ask rule")
	}

	for i := range config.Rules {
		if config.Rules[i].ID == "destructive_command_deny" {
			config.Rules[i].Decision = models.PolicyDecisionAllow
		}
	}
	if _, err := service.UpdatePolicyConfig(config, "test"); err == nil {
		t.Fatal("expected protected deny rule relaxation to fail")
	}
}

func TestEnsureSessionAppliesBypassOverride(t *testing.T) {
	storeImpl, err := store.NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	service := NewService(storeImpl, events.NewHub(), nil, nil, log.New(io.Discard, "", 0))

	override := true
	session, err := service.ensureSession(models.Host{ID: "local", Mode: models.HostModeLocal}, "", "check", models.SessionMode{BypassApprovals: false}, &override)
	if err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	if !session.Mode.BypassApprovals {
		t.Fatal("expected bypass override to be applied to new session")
	}
}

func TestSessionDetailOptionsLimitAndCompact(t *testing.T) {
	storeImpl, err := store.NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	service := NewService(storeImpl, events.NewHub(), nil, nil, log.New(io.Discard, "", 0))
	now := time.Now().UTC()
	if err := storeImpl.SaveHost(models.Host{ID: "local", DisplayName: "Local", Mode: models.HostModeLocal, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("save host: %v", err)
	}
	session := models.Session{ID: "session-1", HostID: "local", Title: "history", CreatedAt: now, UpdatedAt: now}
	for i := 0; i < 3; i++ {
		turnID := models.NewID("turn")
		runID := models.NewID("run")
		session.TurnIDs = append(session.TurnIDs, turnID)
		session.RunIDs = append(session.RunIDs, runID)
		turn := models.Turn{
			ID:        turnID,
			RunID:     runID,
			SessionID: session.ID,
			HostID:    "local",
			UserInput: "check",
			ToolResults: []models.ToolExecutionRecord{{
				RawResult: strings.Repeat("x", 5000),
			}},
			CreatedAt: now.Add(time.Duration(i) * time.Second),
			UpdatedAt: now.Add(time.Duration(i) * time.Second),
		}
		run := models.Run{ID: runID, SessionID: session.ID, TurnID: turnID, HostID: "local", Status: models.RunStatusCompleted, CreatedAt: turn.CreatedAt, UpdatedAt: turn.UpdatedAt}
		if err := storeImpl.SaveTurn(turn); err != nil {
			t.Fatalf("save turn: %v", err)
		}
		if err := storeImpl.SaveRun(run); err != nil {
			t.Fatalf("save run: %v", err)
		}
		for j := 0; j < 4; j++ {
			if err := storeImpl.AppendEvent(models.Event{ID: models.NewID("event"), RunID: runID, Type: "run.stdout", Message: strings.Repeat("y", 2000), Timestamp: now}); err != nil {
				t.Fatalf("append event: %v", err)
			}
		}
	}
	if err := storeImpl.SaveSession(session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	detail, found, err := service.GetSessionDetailWithOptions(session.ID, SessionDetailOptions{TurnLimit: 2, EventLimit: 2, Compact: true})
	if err != nil || !found {
		t.Fatalf("detail found=%v err=%v", found, err)
	}
	if len(detail.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(detail.Turns))
	}
	for _, item := range detail.Turns {
		if len(item.Events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(item.Events))
		}
		if len(item.Turn.ToolResults[0].RawResult) > 4100 {
			t.Fatalf("expected compact raw result, got %d chars", len(item.Turn.ToolResults[0].RawResult))
		}
	}
}

func TestSaveAutomationPersistsDefaults(t *testing.T) {
	storeImpl, err := store.NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	service := NewService(storeImpl, events.NewHub(), nil, nil, log.New(io.Discard, "", 0))
	if err := storeImpl.SaveHost(models.Host{ID: "local", DisplayName: "Local", Mode: models.HostModeLocal, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("save host: %v", err)
	}

	rule, err := service.SaveAutomation(models.AutomationRule{
		Name:      "CPU high",
		Enabled:   true,
		HostID:    "local",
		Metric:    "cpu_usage",
		Operator:  ">",
		Threshold: 80,
	})
	if err != nil {
		t.Fatalf("save automation: %v", err)
	}
	if rule.TriggerType != models.TriggerTypeThreshold {
		t.Fatalf("unexpected trigger type: %+v", rule)
	}
	if rule.WindowMinutes <= 0 || rule.CooldownMinutes <= 0 {
		t.Fatalf("expected default timings, got %+v", rule)
	}
}

func TestAutomationSampleAndForcedTestRun(t *testing.T) {
	storeImpl, err := store.NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := storeImpl.SaveHost(models.Host{ID: "local", DisplayName: "Local", Mode: models.HostModeLocal, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("save host: %v", err)
	}
	executor := runner.NewExecutor(5*time.Second, "")
	registry := builtin.NewRegistry(executor)
	builder := contextbuilder.NewBuilder(nil, registry, policy.New())
	service := NewService(storeImpl, events.NewHub(), builder, gatewayFakeRuntime{}, log.New(io.Discard, "", 0))
	service.SetExecutor(executor)
	service.SetGatewayConfig(models.GatewayConfig{RuntimeSettings: models.DefaultRuntimeSettings()})

	rule, err := service.SaveAutomation(models.AutomationRule{
		Name:            "Disk smoke",
		Enabled:         true,
		HostID:          "local",
		Metric:          "disk_usage",
		Operator:        ">",
		Threshold:       1000,
		CooldownMinutes: 30,
		WindowMinutes:   1,
	})
	if err != nil {
		t.Fatalf("save automation: %v", err)
	}

	sample, err := service.SampleAutomation(context.Background(), rule.ID)
	if err != nil {
		t.Fatalf("sample automation: %v", err)
	}
	if sample.Metric != "disk_usage" || sample.CapturedAt.IsZero() {
		t.Fatalf("unexpected sample: %+v", sample)
	}

	ordinary, err := service.TestAutomation(context.Background(), rule.ID, false)
	if err != nil {
		t.Fatalf("test automation: %v", err)
	}
	if ordinary.RunCreated {
		t.Fatalf("expected no run when threshold is not matched: %+v", ordinary)
	}
	if ordinary.ThresholdMatched {
		t.Fatalf("threshold should not match: %+v", ordinary)
	}

	forced, err := service.TestAutomation(context.Background(), rule.ID, true)
	if err != nil {
		t.Fatalf("forced test automation: %v", err)
	}
	if !forced.RunCreated || forced.Run == nil {
		t.Fatalf("expected forced test to create run: %+v", forced)
	}
	if forced.Run.RequestedBy != "automation_test" {
		t.Fatalf("unexpected requested_by: %+v", forced.Run)
	}
	waitForRunStatus(t, service, forced.Run.ID, models.RunStatusCompleted)
}

func waitForRunStatus(t *testing.T, service *Service, runID string, status string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, found, err := service.GetRun(runID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if found && run.Status == status {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach %s", runID, status)
}

func TestAutomationDeleteRemovesRule(t *testing.T) {
	storeImpl, err := store.NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	service := NewService(storeImpl, events.NewHub(), nil, nil, log.New(io.Discard, "", 0))
	if err := storeImpl.SaveHost(models.Host{ID: "local", DisplayName: "Local", Mode: models.HostModeLocal, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("save host: %v", err)
	}
	rule, err := service.SaveAutomation(models.AutomationRule{Name: "delete me", HostID: "local", Metric: "disk_usage", Operator: ">", Threshold: 80})
	if err != nil {
		t.Fatalf("save automation: %v", err)
	}
	if err := service.DeleteAutomation(rule.ID); err != nil {
		t.Fatalf("delete automation: %v", err)
	}
	if _, found, err := storeImpl.GetAutomation(rule.ID); err != nil || found {
		t.Fatalf("expected deleted rule, found=%v err=%v", found, err)
	}
}
