package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// VLANRepo persists VLAN entities in SQLite.
type VLANRepo struct{ db DBTX }

func NewVLANRepo(db *sql.DB) *VLANRepo          { return &VLANRepo{db: db} }
func (r *VLANRepo) WithTx(tx *sql.Tx) *VLANRepo { return &VLANRepo{db: tx} }
func (r *VLANRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}
func (r *VLANRepo) CreateTx(tx *sql.Tx, v domain.VLAN) (int64, error) {
	return r.WithTx(tx).Create(v)
}
func (r *VLANRepo) UpdateTx(tx *sql.Tx, v domain.VLAN) error { return r.WithTx(tx).Update(v) }
func (r *VLANRepo) GetTx(tx *sql.Tx, id int64) (domain.VLAN, error) {
	return r.WithTx(tx).Get(id)
}

func (r *VLANRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM vlans`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count vlans: %w", err)
	}
	return n, nil
}

func (r *VLANRepo) Create(v domain.VLAN) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO vlans (name, vid, notes) VALUES (?, ?, ?)`,
		v.Name, v.VID, v.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert vlan: %w", err)
	}
	return res.LastInsertId()
}

func (r *VLANRepo) Get(id int64) (domain.VLAN, error) {
	row := r.db.QueryRow(`SELECT id, name, vid, notes FROM vlans WHERE id = ?`, id)
	return scanVLAN(row)
}

func (r *VLANRepo) List() ([]domain.VLAN, error) {
	rows, err := r.db.Query(`SELECT id, name, vid, notes FROM vlans ORDER BY vid`)
	if err != nil {
		return nil, fmt.Errorf("query vlans: %w", err)
	}
	defer rows.Close()
	vlans := []domain.VLAN{}
	for rows.Next() {
		v, err := scanVLAN(rows)
		if err != nil {
			return nil, err
		}
		vlans = append(vlans, v)
	}
	return vlans, rows.Err()
}

func (r *VLANRepo) Update(v domain.VLAN) error {
	res, err := r.db.Exec(
		`UPDATE vlans SET name=?, vid=?, notes=? WHERE id=?`,
		v.Name, v.VID, v.Notes, v.ID,
	)
	if err != nil {
		return fmt.Errorf("update vlan: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

func (r *VLANRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM vlans WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete vlan: %w", err)
	}
	return nil
}

func scanVLAN(s scanner) (domain.VLAN, error) {
	var v domain.VLAN
	if err := s.Scan(&v.ID, &v.Name, &v.VID, &v.Notes); err != nil {
		return domain.VLAN{}, notFound(fmt.Errorf("scan vlan: %w", err))
	}
	return v, nil
}
