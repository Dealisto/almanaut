package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"gopkg.in/yaml.v3"
)

func newTestDB(t *testing.T) *sql.DB {
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
	return db
}

func TestExportEmptyMarshalsEmptyLists(t *testing.T) {
	db := newTestDB(t)
	snap, err := Export(db)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if snap.Version != 1 {
		t.Errorf("Version = %d, want 1", snap.Version)
	}
	out, err := yaml.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"hosts: []", "services: []", "relationships: []", "tags: []"} {
		if !strings.Contains(string(out), key) {
			t.Errorf("empty inventory: want %q (not null) in:\n%s", key, out)
		}
	}
}

func TestTagRepoListAll(t *testing.T) {
	db := newTestDB(t)
	tags := NewTagRepo(db)
	if err := tags.Add(domain.Tag{EntityType: "host", EntityID: 1, Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := tags.Add(domain.Tag{EntityType: "service", EntityID: 1, Name: "b"}); err != nil {
		t.Fatal(err)
	}
	all, err := tags.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d tags, want 2", len(all))
	}
}
