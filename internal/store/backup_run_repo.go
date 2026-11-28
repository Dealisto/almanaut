package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

const BackupRunRetention = 200

var ErrBackupRunConflict = errors.New("run_id already exists with different report data")
var ErrBackupRunTooOld = errors.New("report predates the retained backup history window")

type BackupRunRepo struct{ db *sql.DB }

func NewBackupRunRepo(db *sql.DB) *BackupRunRepo { return &BackupRunRepo{db: db} }

const backupRunColumns = `id, backup_id, run_id, status, started_at, completed_at, duration_seconds, size_bytes, message, received_at`

func scanBackupRun(s scanner) (domain.BackupRun, error) {
	var run domain.BackupRun
	if err := s.Scan(&run.ID, &run.BackupID, &run.RunID, &run.Status, &run.StartedAt, &run.CompletedAt, &run.DurationSeconds, &run.SizeBytes, &run.Message, &run.ReceivedAt); err != nil {
		return run, notFound(fmt.Errorf("scan backup run: %w", err))
	}
	return run, nil
}

// RecordTx returns an identical existing report without writing. Older reports
// outside a full retention window are rejected so late reports cannot evict the
// most recent evidence. Callers commit audit and outbound events in this tx.
func (r *BackupRunRepo) RecordTx(tx *sql.Tx, run domain.BackupRun, now time.Time) (domain.BackupRun, bool, error) {
	normalized, err := domain.NormalizeBackupRun(run, now)
	if err != nil {
		return run, false, &ConstraintError{Reason: err.Error()}
	}
	run = normalized
	if _, err := NewBackupRepo(r.db).GetTx(tx, run.BackupID); err != nil {
		return run, false, err
	}
	existing, err := scanBackupRun(tx.QueryRow(`SELECT `+backupRunColumns+` FROM backup_runs WHERE backup_id=? AND run_id=?`, run.BackupID, run.RunID))
	if err == nil {
		comparable := existing
		comparable.ID = 0
		comparable.ReceivedAt = ""
		incoming := run
		incoming.ID = 0
		incoming.ReceivedAt = ""
		a, _ := json.Marshal(comparable)
		b, _ := json.Marshal(incoming)
		if string(a) != string(b) {
			return run, false, ErrBackupRunConflict
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return run, false, err
	}
	var cutoff string
	err = tx.QueryRow(`SELECT completed_at FROM backup_runs WHERE backup_id=? ORDER BY completed_at DESC,id DESC LIMIT 1 OFFSET ?`, run.BackupID, BackupRunRetention-1).Scan(&cutoff)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return run, false, err
	}
	if cutoff != "" && run.CompletedAt <= cutoff {
		return run, false, ErrBackupRunTooOld
	}
	run.ReceivedAt = now.UTC().Format(time.RFC3339Nano)
	result, err := tx.Exec(`INSERT INTO backup_runs (backup_id,run_id,status,started_at,completed_at,duration_seconds,size_bytes,message,received_at) VALUES (?,?,?,?,?,?,?,?,?)`, run.BackupID, run.RunID, run.Status, run.StartedAt, run.CompletedAt, run.DurationSeconds, run.SizeBytes, run.Message, run.ReceivedAt)
	if err != nil {
		return run, false, fmt.Errorf("insert backup run: %w", err)
	}
	run.ID, err = result.LastInsertId()
	if err != nil {
		return run, false, err
	}
	_, err = tx.Exec(`DELETE FROM backup_runs WHERE backup_id=? AND id NOT IN (SELECT id FROM backup_runs WHERE backup_id=? ORDER BY completed_at DESC,id DESC LIMIT ?)`, run.BackupID, run.BackupID, BackupRunRetention)
	if err != nil {
		return run, false, fmt.Errorf("prune backup run history: %w", err)
	}
	return run, true, nil
}

func (r *BackupRunRepo) List(ctx context.Context, backupID int64) ([]domain.BackupRun, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+backupRunColumns+` FROM backup_runs WHERE backup_id=? ORDER BY completed_at DESC,id DESC LIMIT ?`, backupID, BackupRunRetention)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []domain.BackupRun{}
	for rows.Next() {
		run, err := scanBackupRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (r *BackupRunRepo) LatestByBackup(ctx context.Context) (map[int64]domain.BackupRun, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+backupRunColumns+` FROM backup_runs r WHERE id=(SELECT id FROM backup_runs latest WHERE latest.backup_id=r.backup_id ORDER BY completed_at DESC,id DESC LIMIT 1)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make(map[int64]domain.BackupRun)
	for rows.Next() {
		run, err := scanBackupRun(rows)
		if err != nil {
			return nil, err
		}
		runs[run.BackupID] = run
	}
	return runs, rows.Err()
}
