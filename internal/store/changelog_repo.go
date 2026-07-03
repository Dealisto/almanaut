package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// ChangeEvent is one recorded change, as stored and read back.
type ChangeEvent struct {
	ID         int64
	EntityType string
	EntityID   int64
	Label      string
	Action     string
	Actor      string
	Changes    []domain.FieldChange
	CreatedAt  string
}

// ChangelogRepo persists the append-only changelog.
type ChangelogRepo struct {
	db DBTX
}

// NewChangelogRepo returns a ChangelogRepo backed by db.
func NewChangelogRepo(db *sql.DB) *ChangelogRepo { return &ChangelogRepo{db: db} }

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *ChangelogRepo) WithTx(tx *sql.Tx) *ChangelogRepo { return &ChangelogRepo{db: tx} }

// Create inserts one change event. Changes is marshalled to JSON.
func (r *ChangelogRepo) Create(e ChangeEvent) error {
	raw, err := json.Marshal(e.Changes)
	if err != nil {
		return fmt.Errorf("marshal changes: %w", err)
	}
	if _, err := r.db.Exec(
		`INSERT INTO changelog (entity_type, entity_id, label, action, actor, changes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.EntityType, e.EntityID, e.Label, e.Action, e.Actor, string(raw), e.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert changelog: %w", err)
	}
	return nil
}

// ListForEntity returns the events for (entityType, entityID), newest first.
func (r *ChangelogRepo) ListForEntity(entityType string, entityID int64) ([]ChangeEvent, error) {
	return r.query(
		`SELECT id, entity_type, entity_id, label, action, actor, changes, created_at
		 FROM changelog WHERE entity_type = ? AND entity_id = ? ORDER BY id DESC`,
		entityType, entityID,
	)
}

// ListRecent returns the most recent limit events, newest first.
func (r *ChangelogRepo) ListRecent(limit int) ([]ChangeEvent, error) {
	return r.query(
		`SELECT id, entity_type, entity_id, label, action, actor, changes, created_at
		 FROM changelog ORDER BY id DESC LIMIT ?`,
		limit,
	)
}

func (r *ChangelogRepo) query(sqlStr string, args ...any) ([]ChangeEvent, error) {
	rows, err := r.db.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("query changelog: %w", err)
	}
	defer rows.Close()
	events := []ChangeEvent{}
	for rows.Next() {
		var e ChangeEvent
		var changesJSON string
		if err := rows.Scan(
			&e.ID, &e.EntityType, &e.EntityID, &e.Label, &e.Action, &e.Actor, &changesJSON, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan changelog: %w", err)
		}
		if changesJSON != "" && changesJSON != "null" {
			if err := json.Unmarshal([]byte(changesJSON), &e.Changes); err != nil {
				return nil, fmt.Errorf("unmarshal changes: %w", err)
			}
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
