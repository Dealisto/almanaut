package store

import (
	"path/filepath"
	"testing"
)

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
