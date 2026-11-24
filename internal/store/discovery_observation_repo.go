package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// DiscoveryObservationRepo stores the last successful observation of bound
// resources. Missing resources and failed scans never change these timestamps.
type DiscoveryObservationRepo struct{ db DBTX }

func NewDiscoveryObservationRepo(db *sql.DB) *DiscoveryObservationRepo {
	return &DiscoveryObservationRepo{db: db}
}

func (r *DiscoveryObservationRepo) WithTx(tx *sql.Tx) *DiscoveryObservationRepo {
	return &DiscoveryObservationRepo{db: tx}
}

// RecordBound records a complete successful scan, ignoring IDs that have not
// been imported. Callers must not pass partial results from a failed scan.
func (r *DiscoveryObservationRepo) RecordBound(source string, ids []string, observedAt time.Time) error {
	if strings.TrimSpace(source) == "" || observedAt.IsZero() {
		return fmt.Errorf("observation source and time are required")
	}
	if db, ok := r.db.(*sql.DB); ok {
		return WithTx(db, func(tx *sql.Tx) error { return r.WithTx(tx).RecordBound(source, ids, observedAt) })
	}
	stamp := observedAt.UTC().Format(time.RFC3339)
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		_, err := r.db.Exec(`INSERT INTO discovery_observations (source, external_id, last_seen)
			SELECT source, external_id, ? FROM discovery_bindings WHERE source = ? AND external_id = ?
			ON CONFLICT (source, external_id) DO UPDATE SET last_seen = excluded.last_seen
			WHERE excluded.last_seen > discovery_observations.last_seen`, stamp, source, id)
		if err != nil {
			return fmt.Errorf("record discovery observation: %w", err)
		}
	}
	return nil
}

// ListSource returns UTC RFC3339 last-seen timestamps by external resource ID.
func (r *DiscoveryObservationRepo) ListSource(source string) (map[string]string, error) {
	rows, err := r.db.Query(`SELECT external_id, last_seen FROM discovery_observations WHERE source = ?`, source)
	if err != nil {
		return nil, fmt.Errorf("list discovery observations: %w", err)
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var id, stamp string
		if err := rows.Scan(&id, &stamp); err != nil {
			return nil, err
		}
		out[id] = stamp
	}
	return out, rows.Err()
}
