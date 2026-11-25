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
// The state also records the notified deadline so renewing an item within
// the alert window re-arms delivery without requiring an intervening pass.
type SentKey struct {
	Kind string
	ID   int64
}

// Sent returns the last successfully notified deadline for each entity.
func (r *NotificationRepo) Sent() (map[SentKey]string, error) {
	rows, err := r.db.Query(`SELECT kind, entity_id, notified_date FROM notification_state`)
	if err != nil {
		return nil, fmt.Errorf("list notification state: %w", err)
	}
	defer rows.Close()
	out := map[SentKey]string{}
	for rows.Next() {
		var k SentKey
		var date string
		if err := rows.Scan(&k.Kind, &k.ID, &date); err != nil {
			return nil, fmt.Errorf("scan notification state: %w", err)
		}
		out[k] = date
	}
	return out, rows.Err()
}

// Mark records that (kind, id)'s deadline was notified at t. It is idempotent.
func (r *NotificationRepo) Mark(kind string, id int64, date string, t time.Time) error {
	_, err := r.db.Exec(
		`INSERT OR REPLACE INTO notification_state (kind, entity_id, notified_date, notified_at)
		 VALUES (?, ?, ?, ?)`,
		kind, id, date, t.UTC().Format(time.RFC3339),
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
