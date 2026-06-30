package store

import (
	"path/filepath"
	"testing"
)

func TestMigrateCreatesSchemaAndIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate (first): %v", err)
	}

	// hosts table must exist
	var name string
	err = db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='hosts'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("hosts table not found: %v", err)
	}

	// migration must be recorded
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE version='0001_init.sql'`,
	).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if count != 1 {
		t.Fatalf("migration count = %d, want 1", count)
	}

	// running again must not error or double-apply
	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate (second): %v", err)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE version='0001_init.sql'`,
	).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query (2): %v", err)
	}
	if count != 1 {
		t.Fatalf("migration count after re-run = %d, want 1", count)
	}
}

func TestMigrateCreatesIndexes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, idx := range []string{
		"idx_relationships_from",
		"idx_relationships_to",
		"idx_tags_entity",
	} {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %q not found: %v", idx, err)
		}
	}
}
