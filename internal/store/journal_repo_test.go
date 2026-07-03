package store

import (
	"errors"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestJournalRepoCRUD(t *testing.T) {
	db := newTestDB(t)
	repo := NewJournalRepo(db)

	id1, err := repo.Create(domain.JournalEntry{EntityType: "host", EntityID: 1, Kind: domain.JournalInfo, Body: "installed", CreatedAt: "2026-05-15T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(domain.JournalEntry{EntityType: "host", EntityID: 1, Kind: domain.JournalIncident, Body: "disk failed", CreatedAt: "2026-07-02T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(domain.JournalEntry{EntityType: "host", EntityID: 2, Kind: domain.JournalInfo, Body: "other host", CreatedAt: "2026-07-02T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}

	list, err := repo.ListForEntity("host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Body != "disk failed" { // newest first
		t.Fatalf("ListForEntity wrong: %+v", list)
	}

	got, err := repo.Get(id1)
	if err != nil || got.Body != "installed" {
		t.Fatalf("Get wrong: %+v err=%v", got, err)
	}

	if err := repo.Delete(id1); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(id1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}

	if err := repo.DeleteByEntity("host", 1); err != nil {
		t.Fatal(err)
	}
	rest, err := repo.ListForEntity("host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 0 {
		t.Fatalf("DeleteByEntity left %d entries", len(rest))
	}
	// entity 2 untouched
	all, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("want 1 remaining entry, got %d", len(all))
	}
}
