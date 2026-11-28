package snmp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Manager serializes scheduled/manual scans of one source and preserves the
// last successful snapshot when a subsequent collection fails.
type Manager struct {
	db              *sql.DB
	id, fingerprint string
	scanner         Scanner
	gate            chan struct{}
}

func NewManager(db *sql.DB, id string, scanner Scanner, fingerprint ...string) *Manager {
	m := &Manager{db: db, id: id, scanner: scanner, gate: make(chan struct{}, 1)}
	if len(fingerprint) > 0 {
		m.fingerprint = fingerprint[0]
	}
	return m
}
func (m *Manager) Run(ctx context.Context) error { _, err := m.Scan(ctx); return err }
func (m *Manager) Scan(ctx context.Context) (Observation, error) {
	select {
	case m.gate <- struct{}{}:
		defer func() { <-m.gate }()
	case <-ctx.Done():
		return Observation{}, ctx.Err()
	}
	// A canceled caller waiting for another scan did not attempt a device read.
	if err := ctx.Err(); err != nil {
		return Observation{}, err
	}
	observation, err := m.scanner.Scan(ctx)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return Observation{}, m.recordFailure(ctx, err)
	}
	payload, err := json.Marshal(observation)
	if err != nil || len(payload) > 2<<20 {
		return Observation{}, m.recordFailure(ctx, errors.New("oversized observation"))
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = m.db.ExecContext(ctx, `INSERT INTO snmp_observations(source_id,config_fingerprint,observed_at,attempted_at,last_error,payload) VALUES(?,?,?,?,'',?) ON CONFLICT(source_id) DO UPDATE SET config_fingerprint=excluded.config_fingerprint,observed_at=excluded.observed_at,attempted_at=excluded.attempted_at,last_error='',payload=excluded.payload`, m.id, m.fingerprint, now, now, string(payload))
	if err != nil {
		return Observation{}, m.recordFailure(ctx, err)
	}
	return observation, nil
}

func (m *Manager) recordFailure(parent context.Context, cause error) error {
	message := "SNMP scan failed; check device availability, access, credentials and collection limits."
	if errors.Is(cause, context.Canceled) {
		message = "SNMP scan was canceled; the previous successful observation is preserved."
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		message = "SNMP scan timed out; the previous successful observation is preserved."
	}
	// A started scheduled scan must leave a failed attempt even when its job
	// deadline expires. The detached write is independently bounded.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Second)
	defer cancel()
	_, err := m.db.ExecContext(ctx, `INSERT INTO snmp_observations(source_id,config_fingerprint,attempted_at,last_error) VALUES(?,?,?,?) ON CONFLICT(source_id) DO UPDATE SET attempted_at=excluded.attempted_at,last_error=excluded.last_error`, m.id, m.fingerprint, time.Now().UTC().Format(time.RFC3339Nano), message)
	if err != nil {
		return fmt.Errorf("cannot save SNMP scan failure status")
	}
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return cause
	}
	return errors.New(message)
}

type Snapshot struct {
	ObservedAt, AttemptedAt, LastError string
	Observation                        Observation
	ConfigurationChanged               bool
}

func Load(ctx context.Context, db *sql.DB, id string, expectedFingerprint ...string) (Snapshot, error) {
	var s Snapshot
	var payload, fingerprint string
	err := db.QueryRowContext(ctx, `SELECT config_fingerprint,observed_at,attempted_at,last_error,CASE WHEN length(payload)<=2097152 THEN payload ELSE '' END FROM snmp_observations WHERE source_id=?`, id).Scan(&fingerprint, &s.ObservedAt, &s.AttemptedAt, &s.LastError, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if len(expectedFingerprint) > 0 && fingerprint != expectedFingerprint[0] {
		s.ConfigurationChanged = true
		s.ObservedAt = ""
		s.LastError = "Source configuration changed; previous observations are hidden until a successful scan. " + s.LastError
		return s, nil
	}
	if err := json.Unmarshal([]byte(payload), &s.Observation); err != nil {
		return s, fmt.Errorf("invalid stored SNMP observation")
	}
	return s, nil
}
