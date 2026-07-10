package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

// CertProbeRepo persists the latest TLS probe result per certificate.
type CertProbeRepo struct {
	db DBTX
}

func NewCertProbeRepo(db *sql.DB) *CertProbeRepo { return &CertProbeRepo{db: db} }

func (r *CertProbeRepo) WithTx(tx *sql.Tx) *CertProbeRepo { return &CertProbeRepo{db: tx} }

// Upsert writes the latest probe status for certID.
func (r *CertProbeRepo) Upsert(certID int64, s domain.CertProbeStatus) error {
	sans, err := json.Marshal(nonNil(s.SANs))
	if err != nil {
		return fmt.Errorf("marshal sans: %w", err)
	}
	mism, err := json.Marshal(nonNil(s.Mismatches))
	if err != nil {
		return fmt.Errorf("marshal mismatches: %w", err)
	}
	_, err = r.db.Exec(
		`INSERT INTO cert_probe_state (certificate_id, probed_at, success, last_error, serial, issuer, sans, not_after, mismatches)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(certificate_id) DO UPDATE SET
		   probed_at=excluded.probed_at, success=excluded.success, last_error=excluded.last_error,
		   serial=excluded.serial, issuer=excluded.issuer, sans=excluded.sans,
		   not_after=excluded.not_after, mismatches=excluded.mismatches`,
		certID, s.ProbedAt.UTC().Format(time.RFC3339), boolToInt(s.Success), s.LastError,
		s.Serial, s.Issuer, string(sans), s.NotAfter, string(mism),
	)
	if err != nil {
		return fmt.Errorf("upsert cert probe: %w", err)
	}
	return nil
}

// Get returns the stored status, or store.ErrNotFound when none exists.
func (r *CertProbeRepo) Get(certID int64) (domain.CertProbeStatus, error) {
	row := r.db.QueryRow(
		`SELECT probed_at, success, last_error, serial, issuer, sans, not_after, mismatches
		 FROM cert_probe_state WHERE certificate_id = ?`, certID,
	)
	var s domain.CertProbeStatus
	var probedAt, sans, mism string
	var success int64
	if err := row.Scan(&probedAt, &success, &s.LastError, &s.Serial, &s.Issuer, &sans, &s.NotAfter, &mism); err != nil {
		return domain.CertProbeStatus{}, notFound(fmt.Errorf("scan cert probe: %w", err))
	}
	s.Success = success != 0
	s.ProbedAt, _ = time.Parse(time.RFC3339, probedAt)
	if err := json.Unmarshal([]byte(sans), &s.SANs); err != nil {
		return domain.CertProbeStatus{}, fmt.Errorf("unmarshal sans: %w", err)
	}
	if err := json.Unmarshal([]byte(mism), &s.Mismatches); err != nil {
		return domain.CertProbeStatus{}, fmt.Errorf("unmarshal mismatches: %w", err)
	}
	return s, nil
}

// nonNil returns a non-nil slice so json.Marshal emits [] not null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
