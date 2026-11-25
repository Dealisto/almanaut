package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenPreservesLiteralDatabasePath(t *testing.T) {
	for _, name := range []string{"inventory?mode=memory.db", "inventory#archive.db", "inventory%3Fbackup.db", "inventory with spaces.db"} {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.Contains(name, "?") {
				t.Skip("Windows filenames cannot contain question marks")
			}
			path := filepath.Join(t.TempDir(), name)
			db, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TABLE marker (value TEXT); INSERT INTO marker VALUES ('persisted')`); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("database was not created at its literal path: %v", err)
			}
			db, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var value, journal string
			var foreignKeys, busyTimeout int
			if err := db.QueryRow(`SELECT value FROM marker`).Scan(&value); err != nil || value != "persisted" {
				t.Fatalf("reopened database: value=%q error=%v", value, err)
			}
			if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil || journal != "wal" {
				t.Fatalf("journal mode=%q error=%v", journal, err)
			}
			if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
				t.Fatalf("foreign keys=%d error=%v", foreignKeys, err)
			}
			if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil || busyTimeout != 5000 {
				t.Fatalf("busy timeout=%d error=%v", busyTimeout, err)
			}
		})
	}
}

func TestOpenDoesNotCapConnectionPool(t *testing.T) {
	// The pool must stay at its default (0 = unlimited). Capping it at one
	// connection deadlocks reads issued while a transaction is open on the same
	// DB (see TestWithTxBoundRepoIsolatedUntilCommit), which the code relies on.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if got := db.Stats().MaxOpenConnections; got != 0 {
		t.Fatalf("MaxOpenConnections = %d, want 0 (uncapped); a one-connection pool deadlocks read-during-transaction", got)
	}
}

func TestOpenFailsOnUnwritablePath(t *testing.T) {
	// A path whose parent directory does not exist cannot be created, so the
	// ping must surface the failure rather than deferring it to first query.
	dbPath := filepath.Join(t.TempDir(), "missing-dir", "test.db")
	db, err := Open(dbPath)
	if err == nil {
		db.Close()
		t.Fatal("Open: expected error for unwritable path, got nil")
	}
}
