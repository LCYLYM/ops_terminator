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
