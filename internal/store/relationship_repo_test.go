package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newRelationshipRepo(t *testing.T) *RelationshipRepo {
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
	return NewRelationshipRepo(db)
}

func TestRelationshipRepoCRUDAndListByTo(t *testing.T) {
	repo := newRelationshipRepo(t)

	id, err := repo.Create(domain.Relationship{
		FromType: "service", FromID: 1, ToType: "host", ToID: 2, Kind: "runs on",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == 0 {
		t.Fatal("Create returned id 0")
	}
	if _, err := repo.Create(domain.Relationship{
		FromType: "domain", FromID: 5, ToType: "service", ToID: 1, Kind: "exposed via",
	}); err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	all, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List len = %d, want 2", len(all))
	}

	toHost, err := repo.ListByTo("host", 2)
	if err != nil {
		t.Fatalf("ListByTo: %v", err)
	}
	if len(toHost) != 1 || toHost[0].FromType != "service" || toHost[0].FromID != 1 {
		t.Errorf("ListByTo(host,2) = %+v", toHost)
	}

	if err := repo.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	all, _ = repo.List()
	if len(all) != 1 {
		t.Fatalf("List len after delete = %d, want 1", len(all))
	}
}
