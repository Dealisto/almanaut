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

// LastActivity returns, per entity, the timestamp of its most recent changelog
// event (create/update/import/acknowledge/…) as an RFC3339 string. Timestamps
// are written in UTC (see nowRFC3339), so MAX over the text column is also the
// chronological maximum. Entities with no changelog event are absent from the
// map. It backs the stale-entity audit rule with a single grouped query.
func (r *ChangelogRepo) LastActivity() (map[domain.EntityRef]string, error) {
	rows, err := r.db.Query(
		`SELECT entity_type, entity_id, MAX(created_at)
		 FROM changelog GROUP BY entity_type, entity_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query last activity: %w", err)
	}
	defer rows.Close()
	out := map[domain.EntityRef]string{}
	for rows.Next() {
		var ref domain.EntityRef
		var last string
		if err := rows.Scan(&ref.Type, &ref.ID, &last); err != nil {
			return nil, fmt.Errorf("scan last activity: %w", err)
		}
		out[ref] = last
	}
	return out, rows.Err()
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
