package store

import (
	"database/sql"
	"fmt"
)

// KumaRepo persists the almanaut-service → Uptime-Kuma-monitor mapping that
// makes the sync idempotent. A monitor is "managed" iff its id appears here.
type KumaRepo struct{ db DBTX }

func NewKumaRepo(db *sql.DB) *KumaRepo { return &KumaRepo{db: db} }

// All returns the full mapping, serviceID → monitorID.
func (r *KumaRepo) All() (map[int64]int64, error) {
	rows, err := r.db.Query(`SELECT service_id, monitor_id FROM kuma_monitors`)
	if err != nil {
		return nil, fmt.Errorf("query kuma_monitors: %w", err)
	}
	defer rows.Close()
	m := map[int64]int64{}
	for rows.Next() {
		var sid, mid int64
		if err := rows.Scan(&sid, &mid); err != nil {
			return nil, fmt.Errorf("scan kuma_monitors: %w", err)
		}
		m[sid] = mid
	}
	return m, rows.Err()
}

// Put records (or re-points) the monitor managed for a service.
func (r *KumaRepo) Put(serviceID, monitorID int64) error {
	_, err := r.db.Exec(
		`INSERT INTO kuma_monitors (service_id, monitor_id) VALUES (?, ?)
		 ON CONFLICT(service_id) DO UPDATE SET monitor_id = excluded.monitor_id`,
		serviceID, monitorID,
	)
	if err != nil {
		return fmt.Errorf("put kuma_monitor: %w", err)
	}
	return nil
}

// Delete forgets a service's mapping. Deleting an absent row is not an error.
func (r *KumaRepo) Delete(serviceID int64) error {
	if _, err := r.db.Exec(`DELETE FROM kuma_monitors WHERE service_id = ?`, serviceID); err != nil {
		return fmt.Errorf("delete kuma_monitor: %w", err)
	}
	return nil
}
