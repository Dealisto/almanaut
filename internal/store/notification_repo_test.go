package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNotificationDeadlineMigrationPreservesExistingSuppression(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "0045_notification_deadlines.sql" {
			break
		}
		body, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (?, '2026-07-01')`, entry.Name()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO certificates (id, subject, expires_on) VALUES (1, 'certificate', '2026-07-06');
		INSERT INTO hardware (id, name, warranty_end) VALUES (1, 'hardware', '2026-07-07');
		INSERT INTO subscriptions (id, name, renewal_date) VALUES (1, 'subscription', '2026-07-08');
		INSERT INTO notification_state (kind, entity_id, notified_at) VALUES
		('certificate', 1, '2026-07-01'), ('hardware', 1, '2026-07-01'),
		('subscription', 1, '2026-07-01'), ('certificate', 2, '2026-07-01');
	`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(db, path); err != nil {
			t.Fatal(err)
		}
	}
	sent, err := NewNotificationRepo(db).Sent()
	if err != nil {
		t.Fatal(err)
	}
	want := map[SentKey]string{
		{Kind: "certificate", ID: 1}:  "2026-07-06",
		{Kind: "hardware", ID: 1}:     "2026-07-07",
		{Kind: "subscription", ID: 1}: "2026-07-08",
		{Kind: "certificate", ID: 2}:  "",
	}
	if len(sent) != len(want) {
		t.Fatalf("migrated state = %v, want %v", sent, want)
	}
	for key, date := range want {
		if got, ok := sent[key]; !ok || got != date {
			t.Errorf("migrated %v = %q, present=%v; want %q", key, got, ok, date)
		}
	}
}

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

	if err := repo.Mark("certificate", 7, "2026-07-08", time.Now()); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	sent, err = repo.Sent()
	if err != nil {
		t.Fatalf("Sent: %v", err)
	}
	if sent[SentKey{Kind: "certificate", ID: 7}] != "2026-07-08" {
		t.Fatalf("expected certificate 7 in sent set, got %v", sent)
	}

	// Mark again must not error (idempotent upsert).
	if err := repo.Mark("certificate", 7, "2026-07-08", time.Now()); err != nil {
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
