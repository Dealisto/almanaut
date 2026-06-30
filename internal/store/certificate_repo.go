package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// CertificateRepo persists Certificate entities in SQLite.
type CertificateRepo struct {
	db DBTX
}

// NewCertificateRepo returns a CertificateRepo backed by db.
func NewCertificateRepo(db *sql.DB) *CertificateRepo {
	return &CertificateRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *CertificateRepo) WithTx(tx *sql.Tx) *CertificateRepo {
	return &CertificateRepo{db: tx}
}

// DeleteTx removes the certificate with the given id within tx.
func (r *CertificateRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Create inserts c and returns its new ID.
func (r *CertificateRepo) Create(c domain.Certificate) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO certificates (subject, issuer, expires_on, auto_renew, notes)
		 VALUES (?, ?, ?, ?, ?)`,
		c.Subject, c.Issuer, c.ExpiresOn, boolToInt(c.AutoRenew), c.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert certificate: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the certificate with the given id.
func (r *CertificateRepo) Get(id int64) (domain.Certificate, error) {
	row := r.db.QueryRow(
		`SELECT id, subject, issuer, expires_on, auto_renew, notes FROM certificates WHERE id = ?`, id,
	)
	return scanCertificate(row)
}

// List returns all certificates ordered by expiry date (soonest first).
func (r *CertificateRepo) List() ([]domain.Certificate, error) {
	rows, err := r.db.Query(
		`SELECT id, subject, issuer, expires_on, auto_renew, notes FROM certificates ORDER BY expires_on`,
	)
	if err != nil {
		return nil, fmt.Errorf("query certificates: %w", err)
	}
	defer rows.Close()
	certs := []domain.Certificate{}
	for rows.Next() {
		c, err := scanCertificate(rows)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	return certs, rows.Err()
}

// Update overwrites the certificate with c.ID with the values in c.
func (r *CertificateRepo) Update(c domain.Certificate) error {
	_, err := r.db.Exec(
		`UPDATE certificates SET subject=?, issuer=?, expires_on=?, auto_renew=?, notes=? WHERE id=?`,
		c.Subject, c.Issuer, c.ExpiresOn, boolToInt(c.AutoRenew), c.Notes, c.ID,
	)
	if err != nil {
		return fmt.Errorf("update certificate: %w", err)
	}
	return nil
}

// Delete removes the certificate with the given id.
func (r *CertificateRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM certificates WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete certificate: %w", err)
	}
	return nil
}

func scanCertificate(s scanner) (domain.Certificate, error) {
	var c domain.Certificate
	var autoRenew int64
	if err := s.Scan(&c.ID, &c.Subject, &c.Issuer, &c.ExpiresOn, &autoRenew, &c.Notes); err != nil {
		return domain.Certificate{}, fmt.Errorf("scan certificate: %w", err)
	}
	c.AutoRenew = autoRenew != 0
	return c, nil
}
