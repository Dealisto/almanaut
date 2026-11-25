package store

import (
	"database/sql"
	"errors"
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

const rfc3339 = time.RFC3339Nano

// RecordCheck atomically validates the probed target and records an observation
// only if it started after the last accepted check. The previous status is
// returned so callers notify only for a transition that was actually committed.
func (r *LivenessRepo) RecordCheck(entityType string, entityID int64, address string, s domain.LivenessStatus) (*domain.LivenessStatus, bool, error) {
	if db, ok := r.db.(*sql.DB); ok {
		var previous *domain.LivenessStatus
		var accepted bool
		err := WithTx(db, func(tx *sql.Tx) error {
			var err error
			previous, accepted, err = r.WithTx(tx).recordCheck(entityType, entityID, address, s)
			return err
		})
		return previous, accepted && err == nil, err
	}
	return r.recordCheck(entityType, entityID, address, s)
}

func (r *LivenessRepo) recordCheck(entityType string, entityID int64, address string, s domain.LivenessStatus) (*domain.LivenessStatus, bool, error) {
	var table string
	switch entityType {
	case "host":
		table = "hosts"
	case "service":
		table = "services"
	default:
		return nil, false, fmt.Errorf("invalid liveness entity type %q", entityType)
	}
	var currentAddress string
	err := r.db.QueryRow(`SELECT check_address FROM `+table+` WHERE id = ?`, entityID).Scan(&currentAddress)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil // deleted while the dial was in flight
	}
	if err != nil {
		return nil, false, fmt.Errorf("read liveness target: %w", err)
	}
	if currentAddress == "" || currentAddress != address {
		return nil, false, nil
	}
	var previous *domain.LivenessStatus
	prior, err := r.Get(entityType, entityID)
	if err == nil {
		if !prior.CheckedAt.Before(s.CheckedAt) {
			return nil, false, nil
		}
		previous = &prior
	} else if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	s.ChangedAt = s.CheckedAt
	if previous != nil && previous.Status == s.Status {
		s.ChangedAt = previous.ChangedAt
	}
	if err := r.Upsert(entityType, entityID, s); err != nil {
		return nil, false, err
	}
	return previous, true, nil
}

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
