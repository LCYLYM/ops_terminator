package store

import (
	"testing"
	"time"

	"osagentmvp/internal/models"
)

func TestJSONStoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	store, err := NewJSONStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	host := models.Host{
		ID:          "local",
		DisplayName: "Local",
		Mode:        models.HostModeLocal,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := store.SaveHost(host); err != nil {
		t.Fatalf("save host: %v", err)
	}

	got, found, err := store.GetHost("local")
	if err != nil {
		t.Fatalf("get host: %v", err)
	}
	if !found {
		t.Fatal("expected host to exist")
	}
	if got.DisplayName != "Local" {
		t.Fatalf("unexpected host: %+v", got)
	}
}

func TestJSONStoreGatewayConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	store, err := NewJSONStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	now := time.Now().UTC()
	config := models.GatewayConfig{
		CurrentPresetID: "primary",
		RuntimeSettings: models.DefaultRuntimeSettings(),
		UpdatedAt:       now,
		Presets: []models.GatewayPreset{
			{
				ID:        "primary",
				Name:      "Primary",
				BaseURL:   "https://api.longcat.chat",
				APIKey:    "secret-key",
				Model:     "LongCat-Flash-Thinking-2601",
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
	if err := store.SaveGatewayConfig(config); err != nil {
		t.Fatalf("save gateway config: %v", err)
	}

	got, found, err := store.GetGatewayConfig()
	if err != nil {
		t.Fatalf("get gateway config: %v", err)
	}
	if !found {
		t.Fatal("expected gateway config to exist")
	}
	if got.CurrentPresetID != "primary" || len(got.Presets) != 1 || got.Presets[0].Model != "LongCat-Flash-Thinking-2601" {
		t.Fatalf("unexpected gateway config: %+v", got)
	}
	if got.RuntimeSettings.ContextSoftLimitTokens != models.DefaultRuntimeSettings().ContextSoftLimitTokens {
		t.Fatalf("unexpected runtime settings: %+v", got.RuntimeSettings)
	}
	if got.RuntimeSettings.MaxAgentSteps != models.DefaultRuntimeSettings().MaxAgentSteps {
		t.Fatalf("unexpected max agent steps: %+v", got.RuntimeSettings)
	}
}

func TestJSONStoreAutomationRoundTrip(t *testing.T) {
	root := t.TempDir()
	store, err := NewJSONStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	rule := models.AutomationRule{
		ID:          "automation-1",
		Name:        "cpu high",
		Enabled:     true,
		HostID:      "local",
		TriggerType: models.TriggerTypeThreshold,
		Metric:      "cpu_usage",
		Operator:    ">",
		Threshold:   80,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := store.SaveAutomation(rule); err != nil {
		t.Fatalf("save automation: %v", err)
	}

	got, found, err := store.GetAutomation(rule.ID)
	if err != nil {
		t.Fatalf("get automation: %v", err)
	}
	if !found {
		t.Fatal("expected automation rule to exist")
	}
	if got.Name != rule.Name || got.Metric != rule.Metric {
		t.Fatalf("unexpected automation rule: %+v", got)
	}
}
