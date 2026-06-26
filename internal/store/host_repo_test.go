package store

import (
	"path/filepath"
	"testing"

	"github.com/almanaut/almanaut/internal/domain"
)

func newTestRepo(t *testing.T) *HostRepo {
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
	return NewHostRepo(db)
}

func TestHostRepoCreateGetListDelete(t *testing.T) {
	repo := newTestRepo(t)

	id, err := repo.Create(domain.Host{
		Name: "web01", Type: "vm", OS: "Debian 12", IPs: []string{"10.0.0.5"},
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
	if got.Name != "web01" || got.Type != "vm" {
		t.Errorf("Get returned %+v", got)
	}
	if len(got.IPs) != 1 || got.IPs[0] != "10.0.0.5" {
		t.Errorf("Get IPs = %v, want [10.0.0.5]", got.IPs)
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
