package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestBackupRunRetentionIdempotencyAndOrdering(t *testing.T) {
	db := newTestDB(t)
	backups := NewBackupRepo(db)
	id, err := backups.Create(domain.Backup{Source: "external"})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewBackupRunRepo(db)
	now := time.Now().UTC()
	var first domain.BackupRun
	for i := 0; i < BackupRunRetention+1; i++ {
		run := domain.BackupRun{BackupID: id, RunID: fmt.Sprintf("run-%d", i), Status: "success", CompletedAt: now.Add(time.Duration(i-BackupRunRetention) * time.Minute).Format(time.RFC3339Nano)}
		if err := WithTx(db, func(tx *sql.Tx) error {
			var created bool
			stored, _, e := repo.RecordTx(tx, run, now)
			created = stored.ID != 0
			if !created && e == nil {
				t.Fatal("missing created run")
			}
			if i == 0 {
				first = run
			}
			return e
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := repo.List(context.Background(), id)
	if err != nil || len(list) != BackupRunRetention || list[0].RunID != "run-200" {
		t.Fatalf("list=%d err=%v", len(list), err)
	}
	newest := list[0]
	if err := WithTx(db, func(tx *sql.Tx) error {
		got, created, err := repo.RecordTx(tx, newest, now)
		if err == nil && (created || got.ID != newest.ID) {
			t.Fatal("duplicate not idempotent")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	changed := newest
	changed.Status = "failed"
	if err := WithTx(db, func(tx *sql.Tx) error { _, _, err := repo.RecordTx(tx, changed, now); return err }); !errors.Is(err, ErrBackupRunConflict) {
		t.Fatalf("conflicting retry=%v", err)
	}
	if err := WithTx(db, func(tx *sql.Tx) error { _, _, err := repo.RecordTx(tx, first, now); return err }); !errors.Is(err, ErrBackupRunTooOld) {
		t.Fatalf("old retry=%v", err)
	}
	// A late arrival within the retained window cannot become the latest job.
	late := domain.BackupRun{BackupID: id, RunID: "late", Status: "failed", CompletedAt: now.Add(-time.Minute).Format(time.RFC3339Nano)}
	if err := WithTx(db, func(tx *sql.Tx) error { _, _, err := repo.RecordTx(tx, late, now); return err }); err != nil {
		t.Fatal(err)
	}
	latest, err := repo.LatestByBackup(context.Background())
	if err != nil || latest[id].RunID != "run-200" {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
	if err := backups.Delete(id); err != nil {
		t.Fatal(err)
	}
	list, err = repo.List(context.Background(), id)
	if err != nil || len(list) != 0 {
		t.Fatalf("cascade list=%+v err=%v", list, err)
	}
}

func TestBackupExpectedIntervalOmissionPreserves(t *testing.T) {
	repo := newBackupRepo(t)
	hours := int64(24)
	id, err := repo.Create(domain.Backup{Source: "external", ExpectedIntervalHours: &hours})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(domain.Backup{ID: id, Source: "changed"}); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(id)
	if got.ExpectedIntervalHours == nil || *got.ExpectedIntervalHours != 24 {
		t.Fatalf("legacy update cleared policy: %+v", got)
	}
	zero := int64(0)
	got.ExpectedIntervalHours = &zero
	if err := repo.Update(got); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get(id)
	if got.ExpectedIntervalHours == nil || *got.ExpectedIntervalHours != 0 {
		t.Fatalf("explicit clear failed: %+v", got)
	}
}

func TestBackupRunRejectsPrunedRetryAtEqualTimestampCutoff(t *testing.T) {
	db := newTestDB(t)
	id, err := NewBackupRepo(db).Create(domain.Backup{Source: "external"})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewBackupRunRepo(db)
	now := time.Now().UTC()
	completed := now.Add(-time.Hour).Format(time.RFC3339Nano)
	original := domain.BackupRun{BackupID: id, RunID: "run-0", Status: "success", CompletedAt: completed}
	if err := WithTx(db, func(tx *sql.Tx) error {
		for i := 0; i < BackupRunRetention; i++ {
			run := original
			run.RunID = fmt.Sprintf("run-%d", i)
			if _, _, err := repo.RecordTx(tx, run, now); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	newer := original
	newer.RunID = "newer"
	newer.CompletedAt = now.Format(time.RFC3339Nano)
	if err := WithTx(db, func(tx *sql.Tx) error { _, _, err := repo.RecordTx(tx, newer, now); return err }); err != nil {
		t.Fatal(err)
	}
	if err := WithTx(db, func(tx *sql.Tx) error { _, _, err := repo.RecordTx(tx, original, now); return err }); !errors.Is(err, ErrBackupRunTooOld) {
		t.Fatalf("pruned retry=%v", err)
	}
	latest, _ := repo.LatestByBackup(context.Background())
	if latest[id].RunID != "newer" {
		t.Fatalf("retry changed latest=%+v", latest)
	}
}
