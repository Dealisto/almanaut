package store

import (
	"path/filepath"
	"testing"
	"time"
)

func newNotificationRepo(t *testing.T) *NotificationRepo {
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
	return NewNotificationRepo(db)
}

func TestNotificationRepoMarkSentClear(t *testing.T) {
	repo := newNotificationRepo(t)

	sent, err := repo.Sent()
	if err != nil {
		t.Fatalf("Sent: %v", err)
	}
	if len(sent) != 0 {
		t.Fatalf("fresh state should be empty, got %d", len(sent))
	}

	if err := repo.Mark("certificate", 7, time.Now()); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	sent, err = repo.Sent()
	if err != nil {
		t.Fatalf("Sent: %v", err)
	}
	if !sent[SentKey{Kind: "certificate", ID: 7}] {
		t.Fatalf("expected certificate 7 in sent set, got %v", sent)
	}

	// Mark again must not error (idempotent upsert).
	if err := repo.Mark("certificate", 7, time.Now()); err != nil {
		t.Fatalf("second Mark: %v", err)
	}

	if err := repo.Clear("certificate", 7); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	sent, err = repo.Sent()
	if err != nil {
		t.Fatalf("Sent: %v", err)
	}
	if len(sent) != 0 {
		t.Fatalf("state should be empty after Clear, got %d", len(sent))
	}
}
