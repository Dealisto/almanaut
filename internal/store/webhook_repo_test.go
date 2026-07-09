package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newWebhookTestDB(t *testing.T) *WebhookRepo {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewWebhookRepo(db)
}

func TestWebhookRepoRoundTrip(t *testing.T) {
	repo := newWebhookTestDB(t)

	id, err := repo.Create(domain.Webhook{
		URL: "https://ci.example/hook", Secret: "s3cr3t", Enabled: true,
		EntityTypes: []string{"host", "service"}, Events: []string{"created"},
		CreatedAt: "2026-07-09T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.URL != "https://ci.example/hook" || got.Secret != "s3cr3t" || !got.Enabled {
		t.Errorf("Get returned %+v", got)
	}
	if len(got.EntityTypes) != 2 || got.EntityTypes[0] != "host" || got.EntityTypes[1] != "service" {
		t.Errorf("EntityTypes = %v", got.EntityTypes)
	}
	if len(got.Events) != 1 || got.Events[0] != "created" {
		t.Errorf("Events = %v", got.Events)
	}
}

func TestWebhookRepoListEnabledFiltersDisabled(t *testing.T) {
	repo := newWebhookTestDB(t)
	if _, err := repo.Create(domain.Webhook{URL: "a", Secret: "x", Enabled: true, CreatedAt: "t"}); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	if _, err := repo.Create(domain.Webhook{URL: "b", Secret: "x", Enabled: false, CreatedAt: "t"}); err != nil {
		t.Fatalf("Create b: %v", err)
	}
	enabled, err := repo.ListEnabled()
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}
	if len(enabled) != 1 || enabled[0].URL != "a" {
		t.Errorf("ListEnabled = %v, want only [a]", enabled)
	}
}

func TestWebhookRepoUpdateDelete(t *testing.T) {
	repo := newWebhookTestDB(t)
	id, err := repo.Create(domain.Webhook{URL: "a", Secret: "x", Enabled: true, CreatedAt: "t"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Update(domain.Webhook{ID: id, URL: "a2", Secret: "y", Enabled: false, Events: []string{"deleted"}, CreatedAt: "t"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := repo.Get(id)
	if got.URL != "a2" || got.Enabled || len(got.Events) != 1 || got.Events[0] != "deleted" {
		t.Errorf("after Update: %+v", got)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(id); err != ErrNotFound {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := repo.Update(domain.Webhook{ID: id, URL: "z", Secret: "z", CreatedAt: "t"}); err != ErrNotFound {
		t.Errorf("Update missing = %v, want ErrNotFound", err)
	}
}
