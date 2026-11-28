package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
)

const MaxDiscoverySnapshotCandidates = 10000
const maxDiscoverySnapshotBytes = 8 << 20

// DiscoverySnapshot retains the last complete successful observation separately
// from the latest attempt. Failed or oversized scans never imply disappearance.
type DiscoverySnapshot struct {
	Source                   string
	AttemptedAt, SucceededAt time.Time
	LastError                string
	Candidates               []discovery.Candidate
}

type DiscoverySnapshotRepo struct{ db DBTX }

func NewDiscoverySnapshotRepo(db *sql.DB) *DiscoverySnapshotRepo {
	return &DiscoverySnapshotRepo{db: db}
}
func (r *DiscoverySnapshotRepo) WithTx(tx *sql.Tx) *DiscoverySnapshotRepo {
	return &DiscoverySnapshotRepo{db: tx}
}

// Record stores complete observations only. Attempt ordering uses scan start
// times, so a slower earlier request cannot overwrite a newer observation.
// Error details are deliberately not persisted: upstream errors may contain
// credentials or server response bodies.
func (r *DiscoverySnapshotRepo) Record(source string, attemptedAt time.Time, candidates []discovery.Candidate, scanErr error) error {
	if strings.TrimSpace(source) == "" || attemptedAt.IsZero() || attemptedAt.UnixNano() <= 0 {
		return fmt.Errorf("snapshot source and time are required")
	}
	raw := []byte("[]")
	var limitErr error
	if scanErr == nil {
		if len(candidates) > MaxDiscoverySnapshotCandidates {
			limitErr = fmt.Errorf("discovery snapshot exceeds %d candidates", MaxDiscoverySnapshotCandidates)
		} else {
			var err error
			if candidates != nil {
				raw, err = json.Marshal(candidates)
			}
			if err != nil {
				return fmt.Errorf("encode discovery snapshot: %w", err)
			}
			if len(raw) > maxDiscoverySnapshotBytes {
				limitErr = fmt.Errorf("discovery snapshot exceeds 8 MiB")
			}
		}
	}
	message := ""
	succeeded := int64(0)
	if scanErr != nil {
		message = "The latest source scan failed. Review the source for details."
	} else if limitErr != nil {
		message = limitErr.Error()
	} else {
		succeeded = attemptedAt.UnixNano()
	}
	if succeeded == 0 {
		raw = []byte("[]")
	}
	stamp := attemptedAt.UnixNano()
	_, err := r.db.Exec(`INSERT INTO discovery_snapshots(source,attempted_at,succeeded_at,last_error,candidates)
 VALUES(?,?,?,?,?) ON CONFLICT(source) DO UPDATE SET
 attempted_at=MAX(discovery_snapshots.attempted_at,excluded.attempted_at),
 last_error=CASE WHEN excluded.attempted_at >= discovery_snapshots.attempted_at THEN excluded.last_error ELSE discovery_snapshots.last_error END,
 succeeded_at=MAX(discovery_snapshots.succeeded_at,excluded.succeeded_at),
 candidates=CASE WHEN excluded.succeeded_at > 0 AND excluded.succeeded_at >= discovery_snapshots.succeeded_at THEN excluded.candidates ELSE discovery_snapshots.candidates END`, source, stamp, succeeded, message, string(raw))
	if err != nil {
		return fmt.Errorf("record discovery snapshot: %w", err)
	}
	return limitErr
}

func (r *DiscoverySnapshotRepo) Get(source string) (DiscoverySnapshot, error) {
	s := DiscoverySnapshot{Source: source}
	var attempted, succeeded int64
	var raw string
	err := r.db.QueryRow(`SELECT attempted_at,succeeded_at,last_error,candidates FROM discovery_snapshots WHERE source=?`, source).Scan(&attempted, &succeeded, &s.LastError, &raw)
	if err != nil {
		return s, notFound(err)
	}
	s.AttemptedAt = time.Unix(0, attempted).UTC()
	if succeeded > 0 {
		s.SucceededAt = time.Unix(0, succeeded).UTC()
	}
	if err := json.Unmarshal([]byte(raw), &s.Candidates); err != nil {
		return s, fmt.Errorf("decode discovery snapshot: %w", err)
	}
	return s, nil
}
