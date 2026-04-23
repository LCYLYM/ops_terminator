package gateway

import (
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"osagentmvp/internal/events"
	"osagentmvp/internal/models"
	"osagentmvp/internal/store"
)

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

	_, err = service.ensureSession(models.Host{ID: "ssh-1", Mode: models.HostModeSSH}, existing.ID, "check")
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
		Address:     "47.116.9.81",
		User:        "root",
		PasswordEnv: "HENUlhzlcy@",
	})
	if err != nil {
		t.Fatalf("upsert host: %v", err)
	}
	if host.PasswordEnv != "HENUlhzlcy@" {
		t.Fatalf("unexpected password field: %q", host.PasswordEnv)
	}
}
