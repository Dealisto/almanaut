package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// DomainRepo persists Domain entities in SQLite.
type DomainRepo struct {
	db DBTX
}

// NewDomainRepo returns a DomainRepo backed by db.
func NewDomainRepo(db *sql.DB) *DomainRepo {
	return &DomainRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *DomainRepo) WithTx(tx *sql.Tx) *DomainRepo {
	return &DomainRepo{db: tx}
}

// DeleteTx removes the domain with the given id within tx.
func (r *DomainRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}

// Count returns the number of domains.
func (r *DomainRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM domains`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count domains: %w", err)
	}
	return n, nil
}

// Create inserts d and returns its new ID.
func (r *DomainRepo) Create(d domain.Domain) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO domains (fqdn, provider, notes) VALUES (?, ?, ?)`,
		d.FQDN, d.Provider, d.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert domain: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the domain with the given id.
func (r *DomainRepo) Get(id int64) (domain.Domain, error) {
	row := r.db.QueryRow(`SELECT id, fqdn, provider, notes FROM domains WHERE id = ?`, id)
	return scanDomain(row)
}

// List returns all domains ordered by FQDN.
func (r *DomainRepo) List() ([]domain.Domain, error) {
	rows, err := r.db.Query(`SELECT id, fqdn, provider, notes FROM domains ORDER BY fqdn`)
	if err != nil {
		return nil, fmt.Errorf("query domains: %w", err)
	}
	defer rows.Close()
	domains := []domain.Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	return domains, rows.Err()
}

// Update overwrites the domain with d.ID with the values in d.
func (r *DomainRepo) Update(d domain.Domain) error {
	_, err := r.db.Exec(
		`UPDATE domains SET fqdn=?, provider=?, notes=? WHERE id=?`,
		d.FQDN, d.Provider, d.Notes, d.ID,
	)
	if err != nil {
		return fmt.Errorf("update domain: %w", err)
	}
	return nil
}

// Delete removes the domain with the given id.
func (r *DomainRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM domains WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete domain: %w", err)
	}
	return nil
}

func scanDomain(s scanner) (domain.Domain, error) {
	var d domain.Domain
	if err := s.Scan(&d.ID, &d.FQDN, &d.Provider, &d.Notes); err != nil {
		return domain.Domain{}, fmt.Errorf("scan domain: %w", err)
	}
	return d, nil
}
