package store

import (
	"path/filepath"
	"testing"
)

func TestOpenLimitsConnectionPool(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1 (serialized SQLite writer)", got)
	}
}
