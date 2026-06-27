package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newTagRepo(t *testing.T) *TagRepo {
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
	return NewTagRepo(db)
}

func TestTagRepoAddListDelete(t *testing.T) {
	repo := newTagRepo(t)

	if err := repo.Add(domain.Tag{EntityType: "host", EntityID: 1, Name: "media"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// "#Media" normalizes to "media" — duplicate, must be ignored (no error, no second row)
	if err := repo.Add(domain.Tag{EntityType: "host", EntityID: 1, Name: "#Media"}); err != nil {
		t.Fatalf("Add dup: %v", err)
	}
	if err := repo.Add(domain.Tag{EntityType: "host", EntityID: 1, Name: "critical"}); err != nil {
		t.Fatalf("Add 2: %v", err)
	}
	// a tag on a different entity must not show up
	if err := repo.Add(domain.Tag{EntityType: "service", EntityID: 1, Name: "other"}); err != nil {
		t.Fatalf("Add other: %v", err)
	}

	tags, err := repo.ListForEntity("host", 1)
	if err != nil {
		t.Fatalf("ListForEntity: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("got %d tags, want 2: %+v", len(tags), tags)
	}
	// ordered by name: critical, media
	if tags[0].Name != "critical" || tags[1].Name != "media" {
		t.Errorf("unexpected tags: %+v", tags)
	}

	if err := repo.Delete(tags[0].ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	tags, _ = repo.ListForEntity("host", 1)
	if len(tags) != 1 || tags[0].Name != "media" {
		t.Errorf("after delete: %+v", tags)
	}
}

func TestTagRepoCountsAndListByName(t *testing.T) {
	repo := newTagRepo(t)
	mustAdd := func(et string, id int64, name string) {
		if err := repo.Add(domain.Tag{EntityType: et, EntityID: id, Name: name}); err != nil {
			t.Fatalf("Add(%s,%d,%s): %v", et, id, name, err)
		}
	}
	mustAdd("host", 1, "critical")
	mustAdd("service", 1, "critical")
	mustAdd("service", 2, "media")

	counts, err := repo.Counts()
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	if len(counts) != 2 {
		t.Fatalf("got %d counts, want 2: %+v", len(counts), counts)
	}
	// ordered by name: critical (2), media (1)
	if counts[0].Name != "critical" || counts[0].Count != 2 {
		t.Errorf("counts[0] = %+v, want {critical 2}", counts[0])
	}
	if counts[1].Name != "media" || counts[1].Count != 1 {
		t.Errorf("counts[1] = %+v, want {media 1}", counts[1])
	}

	// ListByName normalizes its argument ("#Critical" -> "critical")
	tagged, err := repo.ListByName("#Critical")
	if err != nil {
		t.Fatalf("ListByName: %v", err)
	}
	if len(tagged) != 2 {
		t.Fatalf("got %d tagged, want 2: %+v", len(tagged), tagged)
	}
	// ordered by entity_type then entity_id: host:1, service:1
	if tagged[0].EntityType != "host" || tagged[1].EntityType != "service" {
		t.Errorf("unexpected order: %+v", tagged)
	}
}
