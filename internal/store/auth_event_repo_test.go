package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newAuthEventRepo(t *testing.T) *AuthEventRepo {
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
	return NewAuthEventRepo(db)
}

func TestAuthEventCreateAndFilter(t *testing.T) {
	r := newAuthEventRepo(t)
	events := []domain.AuthEvent{
		{Type: domain.AuthLoginSuccess, Username: "alice", UserID: 1, SourceIP: "10.0.0.1", CreatedAt: "2026-07-01T10:00:00Z"},
		{Type: domain.AuthLoginFailure, Username: "bob", SourceIP: "10.0.0.2", CreatedAt: "2026-07-05T10:00:00Z"},
		{Type: domain.AuthLoginSuccess, Username: "alice", UserID: 1, SourceIP: "10.0.0.1", CreatedAt: "2026-07-09T10:00:00Z"},
	}
	for _, e := range events {
		if err := r.Create(e); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	// Newest first, no filter.
	all, err := r.List(AuthEventFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 || all[0].CreatedAt != "2026-07-09T10:00:00Z" {
		t.Fatalf("List all = %+v", all)
	}

	// By user.
	byUser, _ := r.List(AuthEventFilter{Username: "alice"})
	if len(byUser) != 2 {
		t.Errorf("by user = %d, want 2", len(byUser))
	}
	// By type.
	byType, _ := r.List(AuthEventFilter{Type: domain.AuthLoginFailure})
	if len(byType) != 1 || byType[0].Username != "bob" {
		t.Errorf("by type = %+v", byType)
	}
	// Date range (bare dates, inclusive of the whole Until day).
	ranged, _ := r.List(AuthEventFilter{Since: "2026-07-02", Until: "2026-07-05"})
	if len(ranged) != 1 || ranged[0].Username != "bob" {
		t.Errorf("date range = %+v", ranged)
	}
}

func TestAuthEventPrune(t *testing.T) {
	r := newAuthEventRepo(t)
	for _, ts := range []string{"2026-01-01T00:00:00Z", "2026-07-09T00:00:00Z"} {
		r.Create(domain.AuthEvent{Type: domain.AuthLoginSuccess, Username: "a", CreatedAt: ts})
	}
	n, err := r.Prune("2026-06-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned = %d, want 1", n)
	}
	all, _ := r.List(AuthEventFilter{})
	if len(all) != 1 || all[0].CreatedAt != "2026-07-09T00:00:00Z" {
		t.Errorf("after prune = %+v", all)
	}
}
