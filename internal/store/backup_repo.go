package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// BackupRepo persists Backup entities in SQLite.
type BackupRepo struct {
	db DBTX
}

// NewBackupRepo returns a BackupRepo backed by db.
func NewBackupRepo(db *sql.DB) *BackupRepo {
	return &BackupRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *BackupRepo) WithTx(tx *sql.Tx) *BackupRepo {
	return &BackupRepo{db: tx}
}

// DeleteTx removes the backup with the given id within tx.
func (r *BackupRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}

// CreateTx inserts b within tx and returns its new id.
func (r *BackupRepo) CreateTx(tx *sql.Tx, b domain.Backup) (int64, error) {
	return r.WithTx(tx).Create(b)
}

// UpdateTx overwrites the backup with b.ID within tx.
func (r *BackupRepo) UpdateTx(tx *sql.Tx, b domain.Backup) error { return r.WithTx(tx).Update(b) }

// GetTx returns the backup with the given id within tx.
func (r *BackupRepo) GetTx(tx *sql.Tx, id int64) (domain.Backup, error) {
	return r.WithTx(tx).Get(id)
}

// Count returns the number of backups.
func (r *BackupRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count backups: %w", err)
	}
	return n, nil
}

// Create inserts b and returns its new ID.
func (r *BackupRepo) Create(b domain.Backup) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO backups (source, destination, frequency, last_run, notes)
		 VALUES (?, ?, ?, ?, ?)`,
		b.Source, b.Destination, b.Frequency, b.LastRun, b.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert backup: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the backup with the given id.
func (r *BackupRepo) Get(id int64) (domain.Backup, error) {
	row := r.db.QueryRow(
		`SELECT id, source, destination, frequency, last_run, notes FROM backups WHERE id = ?`, id,
	)
	return scanBackup(row)
}

// List returns all backups ordered by source.
func (r *BackupRepo) List() ([]domain.Backup, error) {
	rows, err := r.db.Query(
		`SELECT id, source, destination, frequency, last_run, notes FROM backups ORDER BY source`,
	)
	if err != nil {
		return nil, fmt.Errorf("query backups: %w", err)
	}
	defer rows.Close()
	backups := []domain.Backup{}
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		backups = append(backups, b)
	}
	return backups, rows.Err()
}

// Update overwrites the backup with b.ID with the values in b.
func (r *BackupRepo) Update(b domain.Backup) error {
	res, err := r.db.Exec(
		`UPDATE backups SET source=?, destination=?, frequency=?, last_run=?, notes=? WHERE id=?`,
		b.Source, b.Destination, b.Frequency, b.LastRun, b.Notes, b.ID,
	)
	if err != nil {
		return fmt.Errorf("update backup: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes the backup with the given id.
func (r *BackupRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM backups WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete backup: %w", err)
	}
	return nil
}

func scanBackup(s scanner) (domain.Backup, error) {
	var b domain.Backup
	if err := s.Scan(&b.ID, &b.Source, &b.Destination, &b.Frequency, &b.LastRun, &b.Notes); err != nil {
		return domain.Backup{}, notFound(fmt.Errorf("scan backup: %w", err))
	}
	return b, nil
}
