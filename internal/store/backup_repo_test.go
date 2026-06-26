package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newBackupRepo(t *testing.T) *BackupRepo {
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
	return NewBackupRepo(db)
}

func TestBackupRepoCRUD(t *testing.T) {
	repo := newBackupRepo(t)

	id, err := repo.Create(domain.Backup{
		Source: "nextcloud-data", Destination: "Backblaze B2", Frequency: "daily", LastRun: "2026-06-20",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Source != "nextcloud-data" || got.Destination != "Backblaze B2" || got.LastRun != "2026-06-20" {
		t.Errorf("Get returned %+v", got)
	}

	if err := repo.Update(domain.Backup{ID: id, Source: "nextcloud-data", Frequency: "hourly"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = repo.Get(id)
	if got.Frequency != "hourly" {
		t.Errorf("Update not applied: frequency = %q", got.Frequency)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}

	if err := repo.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, _ = repo.List()
	if len(list) != 0 {
		t.Fatalf("List len after delete = %d, want 0", len(list))
	}
}
