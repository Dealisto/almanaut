package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// DiscoveryBindingRepo stores the identity of resources imported from a source.
// Imports must create the inventory entity and binding in the same transaction.
type DiscoveryBindingRepo struct{ db DBTX }

func NewDiscoveryBindingRepo(db *sql.DB) *DiscoveryBindingRepo {
	return &DiscoveryBindingRepo{db: db}
}

func (r *DiscoveryBindingRepo) WithTx(tx *sql.Tx) *DiscoveryBindingRepo {
	return &DiscoveryBindingRepo{db: tx}
}

func (r *DiscoveryBindingRepo) List() ([]domain.DiscoveryBinding, error) {
	rows, err := r.db.Query(`SELECT source, external_id, entity_type, entity_id FROM discovery_bindings ORDER BY source, external_id`)
	if err != nil {
		return nil, fmt.Errorf("list discovery bindings: %w", err)
	}
	defer rows.Close()
	out := []domain.DiscoveryBinding{}
	for rows.Next() {
		var b domain.DiscoveryBinding
		if err := rows.Scan(&b.Source, &b.ExternalID, &b.EntityType, &b.EntityID); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *DiscoveryBindingRepo) Create(b domain.DiscoveryBinding) error {
	if err := b.Validate(); err != nil {
		return &ConstraintError{Reason: err.Error()}
	}
	if db, ok := r.db.(*sql.DB); ok {
		return WithTx(db, func(tx *sql.Tx) error { return r.WithTx(tx).Create(b) })
	}
	table := "hosts"
	if b.EntityType == "service" {
		table = "services"
	}
	var exists bool
	if err := r.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id = ?)`, b.EntityID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return &ConstraintError{Reason: "discovery binding target does not exist"}
	}
	_, err := r.db.Exec(`INSERT INTO discovery_bindings (source, external_id, entity_type, entity_id) VALUES (?, ?, ?, ?)`, b.Source, b.ExternalID, b.EntityType, b.EntityID)
	if err != nil {
		return fmt.Errorf("create discovery binding: %w", err)
	}
	return nil
}
