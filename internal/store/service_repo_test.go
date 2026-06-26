package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newServiceRepo(t *testing.T) *ServiceRepo {
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
	return NewServiceRepo(db)
}

func TestServiceRepoCRUD(t *testing.T) {
	repo := newServiceRepo(t)

	id, err := repo.Create(domain.Service{
		Name: "jellyfin", Kind: "container", URL: "http://10.0.0.20:8096", Ports: "8096", Category: "media",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == 0 {
		t.Fatal("Create returned id 0")
	}

	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "jellyfin" || got.Kind != "container" || got.Ports != "8096" {
		t.Errorf("Get returned %+v", got)
	}

	if err := repo.Update(domain.Service{ID: id, Name: "jellyfin", Kind: "container", Category: "video"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = repo.Get(id)
	if got.Category != "video" {
		t.Errorf("Update not applied: category = %q", got.Category)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}

	if err := repo.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, _ = repo.List()
	if len(list) != 0 {
		t.Fatalf("List len after delete = %d, want 0", len(list))
	}
}
