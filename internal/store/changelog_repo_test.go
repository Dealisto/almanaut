package store

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestChangelogCreateAndList(t *testing.T) {
	db := newTestDB(t) // match the helper used by the other repo tests
	repo := NewChangelogRepo(db)

	must := func(e ChangeEvent) {
		if err := repo.Create(e); err != nil {
			t.Fatal(err)
		}
	}
	must(ChangeEvent{EntityType: "host", EntityID: 1, Label: "nas", Action: domain.ActionCreate, CreatedAt: "2026-07-01T09:00:00Z"})
	must(ChangeEvent{EntityType: "host", EntityID: 1, Label: "nas", Action: domain.ActionUpdate,
		Changes: []domain.FieldChange{{Field: "status", Old: "running", New: "down"}}, CreatedAt: "2026-07-02T14:00:00Z"})
	must(ChangeEvent{EntityType: "cert", EntityID: 5, Label: "*.lan", Action: domain.ActionUpdate, CreatedAt: "2026-07-02T15:00:00Z"})

	forHost, err := repo.ListForEntity("host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(forHost) != 2 {
		t.Fatalf("want 2 host events, got %d", len(forHost))
	}
	if forHost[0].Action != domain.ActionUpdate { // newest first
		t.Errorf("expected newest (update) first, got %q", forHost[0].Action)
	}
	if len(forHost[0].Changes) != 1 || forHost[0].Changes[0].New != "down" {
		t.Errorf("changes not round-tripped: %+v", forHost[0].Changes)
	}

	recent, err := repo.ListRecent(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 {
		t.Fatalf("want 2 recent, got %d", len(recent))
	}
	if recent[0].EntityType != "cert" { // most recent overall
		t.Errorf("expected cert newest, got %q", recent[0].EntityType)
	}
}
