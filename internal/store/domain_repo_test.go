package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newDomainRepo(t *testing.T) *DomainRepo {
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
	return NewDomainRepo(db)
}

func TestDomainRepoCRUD(t *testing.T) {
	repo := newDomainRepo(t)

	id, err := repo.Create(domain.Domain{FQDN: "jellyfin.example.com", Provider: "Cloudflare"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FQDN != "jellyfin.example.com" || got.Provider != "Cloudflare" {
		t.Errorf("Get returned %+v", got)
	}

	if err := repo.Update(domain.Domain{ID: id, FQDN: "media.example.com", Provider: "Cloudflare"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = repo.Get(id)
	if got.FQDN != "media.example.com" {
		t.Errorf("Update not applied: fqdn = %q", got.FQDN)
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
