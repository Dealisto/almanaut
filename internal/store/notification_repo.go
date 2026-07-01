package store

import (
	"database/sql"
	"fmt"
	"time"
)

// NotificationRepo persists the "already notified" state for expiry alerts.
type NotificationRepo struct {
	db DBTX
}

// NewNotificationRepo returns a NotificationRepo backed by db.
func NewNotificationRepo(db *sql.DB) *NotificationRepo {
	return &NotificationRepo{db: db}
}

// SentKey identifies one already-notified entity.
//
// State is keyed on (kind, entity_id): if an entity is deleted while marked
// and a new one reuses its rowid before the next pass, the first notification
// could be suppressed — the clear-on-absence design in Run self-heals this on
// a later pass, once the old key is no longer in the expiring set.
type SentKey struct {
	Kind string
	ID   int64
}

// Sent returns the set of entities that have already been notified.
func (r *NotificationRepo) Sent() (map[SentKey]bool, error) {
	rows, err := r.db.Query(`SELECT kind, entity_id FROM notification_state`)
	if err != nil {
		return nil, fmt.Errorf("list notification state: %w", err)
	}
	defer rows.Close()
	out := map[SentKey]bool{}
	for rows.Next() {
		var k SentKey
		if err := rows.Scan(&k.Kind, &k.ID); err != nil {
			return nil, fmt.Errorf("scan notification state: %w", err)
		}
		out[k] = true
	}
	return out, rows.Err()
}

// Mark records that (kind, id) has been notified at t. It is idempotent.
func (r *NotificationRepo) Mark(kind string, id int64, t time.Time) error {
	_, err := r.db.Exec(
		`INSERT OR REPLACE INTO notification_state (kind, entity_id, notified_at)
		 VALUES (?, ?, ?)`,
		kind, id, t.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("mark notification: %w", err)
	}
	return nil
}

// Clear removes the notified state for (kind, id), re-arming future alerts.
func (r *NotificationRepo) Clear(kind string, id int64) error {
	_, err := r.db.Exec(
		`DELETE FROM notification_state WHERE kind = ? AND entity_id = ?`,
		kind, id,
	)
	if err != nil {
		return fmt.Errorf("clear notification: %w", err)
	}
	return nil
}
