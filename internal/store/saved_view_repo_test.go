package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newSavedViewRepo(t *testing.T) *SavedViewRepo {
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
	return NewSavedViewRepo(db)
}

func TestSavedViewCreateListGrouped(t *testing.T) {
	r := newSavedViewRepo(t)
	for _, v := range []domain.SavedView{
		{UserID: 1, EntityType: "service", Name: "media", Query: "tag=media", CreatedAt: "t"},
		{UserID: 1, EntityType: "host", Name: "down", Query: "field=Status&value=down", CreatedAt: "t"},
		{UserID: 2, EntityType: "host", Name: "other-user", CreatedAt: "t"},
	} {
		if _, err := r.Create(v); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	got, err := r.ListForUser(1)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	// user 1 only, ordered by entity_type then name: host/down, service/media.
	if len(got) != 2 {
		t.Fatalf("got %d views, want 2 (user-scoped): %+v", len(got), got)
	}
	if got[0].EntityType != "host" || got[1].EntityType != "service" {
		t.Errorf("wrong order: %+v", got)
	}
}

func TestSavedViewRenameScoped(t *testing.T) {
	r := newSavedViewRepo(t)
	id, _ := r.Create(domain.SavedView{UserID: 1, EntityType: "host", Name: "old", CreatedAt: "t"})

	// Another user cannot rename it.
	if err := r.Rename(id, 2, "hijack"); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user rename err = %v, want ErrNotFound", err)
	}
	// The owner can.
	if err := r.Rename(id, 1, "new"); err != nil {
		t.Fatalf("owner rename: %v", err)
	}
	got, _ := r.ListForUser(1)
	if len(got) != 1 || got[0].Name != "new" {
		t.Errorf("rename not applied: %+v", got)
	}
}

func TestSavedViewDeleteScoped(t *testing.T) {
	r := newSavedViewRepo(t)
	id, _ := r.Create(domain.SavedView{UserID: 1, EntityType: "host", Name: "v", CreatedAt: "t"})

	if err := r.Delete(id, 2); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user delete err = %v, want ErrNotFound", err)
	}
	if err := r.Delete(id, 1); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	got, _ := r.ListForUser(1)
	if len(got) != 0 {
		t.Errorf("view survived delete: %+v", got)
	}
}
