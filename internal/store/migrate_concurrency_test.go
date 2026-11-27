package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestConcurrentMigrationsApplyEachVersionOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.db")
	const workers = 8
	dbs := make([]*sql.DB, workers)
	for i := range dbs {
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		dbs[i] = db
	}
	start := make(chan struct{})
	results := make(chan error, workers)
	for _, db := range dbs {
		go func() {
			<-start
			results <- Migrate(db, path)
		}()
	}
	close(start)
	for range workers {
		if err := <-results; err != nil {
			t.Errorf("concurrent migration: %v", err)
		}
	}
	files, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := dbs[0].QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(files) {
		t.Fatalf("applied %d migrations, want %d", count, len(files))
	}
}

func TestMigrationSkipsVersionAppliedSincePlanning(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	// A second process can hold a stale pending list after the first finishes.
	// Re-executing this migration would fail because hosts already exists.
	statements, err := migrationsFS.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(db, "0001_init.sql", string(statements)); err != nil {
		t.Fatalf("already applied migration was executed again: %v", err)
	}
}

func TestFailedMigrationRollsBackSchemaAndVersion(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "inventory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(db, "test_failure.sql", `CREATE TABLE partial_migration (id INTEGER); INSERT INTO missing_table VALUES (1)`); err == nil {
		t.Fatal("invalid migration succeeded")
	}
	var tables, versions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'partial_migration'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 'test_failure.sql'`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if tables != 0 || versions != 0 {
		t.Fatalf("failed migration left tables=%d versions=%d", tables, versions)
	}
}
