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

func mustCreateRel(t *testing.T, repo *RelationshipRepo, ft string, fid int64, tt string, tid int64, kind string) {
	t.Helper()
	if _, err := repo.Create(domain.Relationship{FromType: ft, FromID: fid, ToType: tt, ToID: tid, Kind: kind}); err != nil {
		t.Fatalf("Create rel: %v", err)
	}
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

func TestRelationshipListForEntity(t *testing.T) {
	repo := newRelationshipRepo(t)
	mustCreateRel(t, repo, "service", 1, "host", 1, "runs on")   // host:1 is the "to"
	mustCreateRel(t, repo, "host", 1, "network", 2, "connected to") // host:1 is the "from"
	mustCreateRel(t, repo, "domain", 9, "service", 1, "exposed via") // does not touch host:1

	got, err := repo.ListForEntity("host", 1)
	if err != nil {
		t.Fatalf("ListForEntity: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(got), got)
	}
}

func TestRelationshipDeleteByEntity(t *testing.T) {
	repo := newRelationshipRepo(t)
	mustCreateRel(t, repo, "service", 1, "host", 1, "runs on")      // host:1 is the "to"
	mustCreateRel(t, repo, "host", 1, "network", 2, "connected to") // host:1 is the "from"
	mustCreateRel(t, repo, "domain", 9, "service", 1, "exposed via") // does not touch host:1

	if err := repo.DeleteByEntity("host", 1); err != nil {
		t.Fatalf("DeleteByEntity: %v", err)
	}
	all, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("got %d relationships, want 1: %+v", len(all), all)
	}
	if all[0].FromType != "domain" {
		t.Errorf("wrong relationship survived: %+v", all[0])
	}
}
