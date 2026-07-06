package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// LocationRepo persists Location entities in SQLite.
type LocationRepo struct{ db DBTX }

func NewLocationRepo(db *sql.DB) *LocationRepo          { return &LocationRepo{db: db} }
func (r *LocationRepo) WithTx(tx *sql.Tx) *LocationRepo { return &LocationRepo{db: tx} }
func (r *LocationRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}
func (r *LocationRepo) CreateTx(tx *sql.Tx, l domain.Location) (int64, error) {
	return r.WithTx(tx).Create(l)
}
func (r *LocationRepo) UpdateTx(tx *sql.Tx, l domain.Location) error { return r.WithTx(tx).Update(l) }
func (r *LocationRepo) GetTx(tx *sql.Tx, id int64) (domain.Location, error) {
	return r.WithTx(tx).Get(id)
}

func (r *LocationRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM locations`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count locations: %w", err)
	}
	return n, nil
}

func (r *LocationRepo) Create(l domain.Location) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO locations (name, site_id, notes) VALUES (?, ?, ?)`,
		l.Name, l.SiteID, l.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert location: %w", err)
	}
	return res.LastInsertId()
}

func (r *LocationRepo) Get(id int64) (domain.Location, error) {
	row := r.db.QueryRow(`SELECT id, name, site_id, notes FROM locations WHERE id = ?`, id)
	return scanLocation(row)
}

func (r *LocationRepo) List() ([]domain.Location, error) {
	rows, err := r.db.Query(`SELECT id, name, site_id, notes FROM locations ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query locations: %w", err)
	}
	defer rows.Close()
	locations := []domain.Location{}
	for rows.Next() {
		l, err := scanLocation(rows)
		if err != nil {
			return nil, err
		}
		locations = append(locations, l)
	}
	return locations, rows.Err()
}

func (r *LocationRepo) Update(l domain.Location) error {
	res, err := r.db.Exec(
		`UPDATE locations SET name=?, site_id=?, notes=? WHERE id=?`,
		l.Name, l.SiteID, l.Notes, l.ID,
	)
	if err != nil {
		return fmt.Errorf("update location: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

func (r *LocationRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM locations WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete location: %w", err)
	}
	return nil
}

func scanLocation(s scanner) (domain.Location, error) {
	var v domain.Location
	if err := s.Scan(&v.ID, &v.Name, &v.SiteID, &v.Notes); err != nil {
		return domain.Location{}, notFound(fmt.Errorf("scan location: %w", err))
	}
	return v, nil
}
