package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

// LivenessRepo persists the latest liveness check result per entity.
type LivenessRepo struct {
	db DBTX
}

// NewLivenessRepo returns a LivenessRepo backed by db.
func NewLivenessRepo(db *sql.DB) *LivenessRepo { return &LivenessRepo{db: db} }

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *LivenessRepo) WithTx(tx *sql.Tx) *LivenessRepo { return &LivenessRepo{db: tx} }

const rfc3339 = time.RFC3339

// Upsert writes the latest status for (entityType, entityID).
func (r *LivenessRepo) Upsert(entityType string, entityID int64, s domain.LivenessStatus) error {
	_, err := r.db.Exec(
		`INSERT INTO liveness_state (entity_type, entity_id, status, checked_at, changed_at, last_error)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(entity_type, entity_id) DO UPDATE SET
		   status=excluded.status, checked_at=excluded.checked_at,
		   changed_at=excluded.changed_at, last_error=excluded.last_error`,
		entityType, entityID, s.Status,
		s.CheckedAt.UTC().Format(rfc3339), s.ChangedAt.UTC().Format(rfc3339), s.LastError,
	)
	if err != nil {
		return fmt.Errorf("upsert liveness: %w", err)
	}
	return nil
}

// Get returns the stored status, or store.ErrNotFound when none exists.
func (r *LivenessRepo) Get(entityType string, entityID int64) (domain.LivenessStatus, error) {
	row := r.db.QueryRow(
		`SELECT status, checked_at, changed_at, last_error
		 FROM liveness_state WHERE entity_type = ? AND entity_id = ?`,
		entityType, entityID,
	)
	var s domain.LivenessStatus
	var checked, changed string
	if err := row.Scan(&s.Status, &checked, &changed, &s.LastError); err != nil {
		return domain.LivenessStatus{}, notFound(fmt.Errorf("scan liveness: %w", err))
	}
	s.CheckedAt, _ = time.Parse(rfc3339, checked)
	s.ChangedAt, _ = time.Parse(rfc3339, changed)
	return s, nil
}
