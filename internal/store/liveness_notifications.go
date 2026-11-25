package store

import (
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// PendingNotifications returns the earliest pending transition per entity,
// ordered by least recent attempt, so repeatedly failing entities do not starve
// later ones. runID excludes events already attempted in the current pass.
// Later transitions wait for earlier ones, including after a delivery failure.
func (r *LivenessRepo) PendingNotifications(runID string, limit int) ([]domain.LivenessNotification, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := r.db.Query(`SELECT n.id, n.entity_type, n.entity_id, n.label, n.address, n.status, n.last_error, n.observed_at
 FROM liveness_notifications n WHERE (? = '' OR n.last_attempt_run != ?) AND NOT EXISTS (
 SELECT 1 FROM liveness_notifications prior WHERE prior.entity_type=n.entity_type AND prior.entity_id=n.entity_id AND prior.id<n.id)
 ORDER BY n.last_attempt_order, n.id LIMIT ?`, runID, runID, limit)
	if err != nil {
		return nil, fmt.Errorf("list liveness notifications: %w", err)
	}
	defer rows.Close()
	var result []domain.LivenessNotification
	for rows.Next() {
		var n domain.LivenessNotification
		if err := rows.Scan(&n.ID, &n.EntityType, &n.EntityID, &n.Label, &n.Address, &n.Status, &n.LastError, &n.ObservedAt); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// NotificationCurrent rechecks that the event still exists and its address is
// still monitored. Target edits/deletions also remove queued events by trigger.
func (r *LivenessRepo) NotificationCurrent(id int64) (bool, error) {
	var current bool
	err := r.db.QueryRow(`SELECT EXISTS (
 SELECT 1 FROM liveness_notifications n WHERE n.id=? AND (
 (n.entity_type='host' AND EXISTS(SELECT 1 FROM hosts h WHERE h.id=n.entity_id AND h.check_address=n.address AND h.check_address!='')) OR
 (n.entity_type='service' AND EXISTS(SELECT 1 FROM services s WHERE s.id=n.entity_id AND s.check_address=n.address AND s.check_address!=''))))`, id).Scan(&current)
	return current, err
}

// DeleteNotification acknowledges successful delivery. A crash between delivery
// and acknowledgement can repeat an alert; failed sends never acknowledge it.
func (r *LivenessRepo) DeleteNotification(id int64) error {
	_, err := r.db.Exec(`DELETE FROM liveness_notifications WHERE id=?`, id)
	return err
}

// MarkNotificationAttempt records fairness before network I/O, including attempts
// interrupted by a restart. A run identifier prevents same-pass immediate retry.
// A database sequence keeps the order fair even when the wall clock moves back.
func (r *LivenessRepo) MarkNotificationAttempt(id int64, runID string) (bool, error) {
	result, err := r.db.Exec(`UPDATE liveness_notifications SET last_attempt_order=(SELECT COALESCE(MAX(last_attempt_order),0)+1 FROM liveness_notifications), last_attempt_run=? WHERE id=? AND last_attempt_run!=?`, runID, id, runID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}
