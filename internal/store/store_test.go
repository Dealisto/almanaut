package store

import (
	"path/filepath"
	"testing"
)

func TestOpenLimitsToSingleConnection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
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
