package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// RackRepo persists Rack entities in SQLite.
type RackRepo struct{ db DBTX }

func NewRackRepo(db *sql.DB) *RackRepo          { return &RackRepo{db: db} }
func (r *RackRepo) WithTx(tx *sql.Tx) *RackRepo { return &RackRepo{db: tx} }
func (r *RackRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}
func (r *RackRepo) CreateTx(tx *sql.Tx, k domain.Rack) (int64, error) {
	return r.WithTx(tx).Create(k)
}
func (r *RackRepo) UpdateTx(tx *sql.Tx, k domain.Rack) error { return r.WithTx(tx).Update(k) }
func (r *RackRepo) GetTx(tx *sql.Tx, id int64) (domain.Rack, error) {
	return r.WithTx(tx).Get(id)
}

func (r *RackRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM racks`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count racks: %w", err)
	}
	return n, nil
}

func (r *RackRepo) Create(k domain.Rack) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO racks (name, location_id, u_height, notes) VALUES (?, ?, ?, ?)`,
		k.Name, k.LocationID, k.UHeight, k.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert rack: %w", err)
	}
	return res.LastInsertId()
}

func (r *RackRepo) Get(id int64) (domain.Rack, error) {
	row := r.db.QueryRow(`SELECT id, name, location_id, u_height, notes FROM racks WHERE id = ?`, id)
	return scanRack(row)
}

func (r *RackRepo) List() ([]domain.Rack, error) {
	rows, err := r.db.Query(`SELECT id, name, location_id, u_height, notes FROM racks ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query racks: %w", err)
	}
	defer rows.Close()
	racks := []domain.Rack{}
	for rows.Next() {
		k, err := scanRack(rows)
		if err != nil {
			return nil, err
		}
		racks = append(racks, k)
	}
	return racks, rows.Err()
}

func (r *RackRepo) Update(k domain.Rack) error {
	res, err := r.db.Exec(
		`UPDATE racks SET name=?, location_id=?, u_height=?, notes=? WHERE id=?`,
		k.Name, k.LocationID, k.UHeight, k.Notes, k.ID,
	)
	if err != nil {
		return fmt.Errorf("update rack: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

func (r *RackRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM racks WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete rack: %w", err)
	}
	return nil
}

func scanRack(s scanner) (domain.Rack, error) {
	var v domain.Rack
	if err := s.Scan(&v.ID, &v.Name, &v.LocationID, &v.UHeight, &v.Notes); err != nil {
		return domain.Rack{}, notFound(fmt.Errorf("scan rack: %w", err))
	}
	return v, nil
}
