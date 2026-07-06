package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// SiteRepo persists Site entities in SQLite.
type SiteRepo struct{ db DBTX }

func NewSiteRepo(db *sql.DB) *SiteRepo          { return &SiteRepo{db: db} }
func (r *SiteRepo) WithTx(tx *sql.Tx) *SiteRepo { return &SiteRepo{db: tx} }
func (r *SiteRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}
func (r *SiteRepo) CreateTx(tx *sql.Tx, s domain.Site) (int64, error) {
	return r.WithTx(tx).Create(s)
}
func (r *SiteRepo) UpdateTx(tx *sql.Tx, s domain.Site) error { return r.WithTx(tx).Update(s) }
func (r *SiteRepo) GetTx(tx *sql.Tx, id int64) (domain.Site, error) {
	return r.WithTx(tx).Get(id)
}

func (r *SiteRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM sites`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count sites: %w", err)
	}
	return n, nil
}

func (r *SiteRepo) Create(s domain.Site) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO sites (name, address, notes) VALUES (?, ?, ?)`,
		s.Name, s.Address, s.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert site: %w", err)
	}
	return res.LastInsertId()
}

func (r *SiteRepo) Get(id int64) (domain.Site, error) {
	row := r.db.QueryRow(`SELECT id, name, address, notes FROM sites WHERE id = ?`, id)
	return scanSite(row)
}

func (r *SiteRepo) List() ([]domain.Site, error) {
	rows, err := r.db.Query(`SELECT id, name, address, notes FROM sites ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query sites: %w", err)
	}
	defer rows.Close()
	sites := []domain.Site{}
	for rows.Next() {
		s, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		sites = append(sites, s)
	}
	return sites, rows.Err()
}

func (r *SiteRepo) Update(s domain.Site) error {
	res, err := r.db.Exec(
		`UPDATE sites SET name=?, address=?, notes=? WHERE id=?`,
		s.Name, s.Address, s.Notes, s.ID,
	)
	if err != nil {
		return fmt.Errorf("update site: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

func (r *SiteRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM sites WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete site: %w", err)
	}
	return nil
}

func scanSite(s scanner) (domain.Site, error) {
	var v domain.Site
	if err := s.Scan(&v.ID, &v.Name, &v.Address, &v.Notes); err != nil {
		return domain.Site{}, notFound(fmt.Errorf("scan site: %w", err))
	}
	return v, nil
}
