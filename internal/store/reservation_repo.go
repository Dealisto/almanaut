package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// ReservationRepo persists IP Reservation entities in SQLite.
type ReservationRepo struct{ db DBTX }

func NewReservationRepo(db *sql.DB) *ReservationRepo          { return &ReservationRepo{db: db} }
func (r *ReservationRepo) WithTx(tx *sql.Tx) *ReservationRepo { return &ReservationRepo{db: tx} }
func (r *ReservationRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}
func (r *ReservationRepo) CreateTx(tx *sql.Tx, v domain.Reservation) (int64, error) {
	return r.WithTx(tx).Create(v)
}
func (r *ReservationRepo) UpdateTx(tx *sql.Tx, v domain.Reservation) error {
	return r.WithTx(tx).Update(v)
}
func (r *ReservationRepo) GetTx(tx *sql.Tx, id int64) (domain.Reservation, error) {
	return r.WithTx(tx).Get(id)
}

func (r *ReservationRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM ip_reservations`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count reservations: %w", err)
	}
	return n, nil
}

func (r *ReservationRepo) Create(v domain.Reservation) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO ip_reservations (network_id, name, start_ip, end_ip, notes) VALUES (?, ?, ?, ?, ?)`,
		v.NetworkID, v.Name, v.StartIP, v.EndIP, v.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert reservation: %w", err)
	}
	return res.LastInsertId()
}

func (r *ReservationRepo) Get(id int64) (domain.Reservation, error) {
	row := r.db.QueryRow(`SELECT id, network_id, name, start_ip, end_ip, notes FROM ip_reservations WHERE id = ?`, id)
	return scanReservation(row)
}

func (r *ReservationRepo) List() ([]domain.Reservation, error) {
	rows, err := r.db.Query(`SELECT id, network_id, name, start_ip, end_ip, notes FROM ip_reservations ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query reservations: %w", err)
	}
	defer rows.Close()
	items := []domain.Reservation{}
	for rows.Next() {
		v, err := scanReservation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (r *ReservationRepo) Update(v domain.Reservation) error {
	res, err := r.db.Exec(
		`UPDATE ip_reservations SET network_id=?, name=?, start_ip=?, end_ip=?, notes=? WHERE id=?`,
		v.NetworkID, v.Name, v.StartIP, v.EndIP, v.Notes, v.ID,
	)
	if err != nil {
		return fmt.Errorf("update reservation: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

func (r *ReservationRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM ip_reservations WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete reservation: %w", err)
	}
	return nil
}

func scanReservation(s scanner) (domain.Reservation, error) {
	var v domain.Reservation
	if err := s.Scan(&v.ID, &v.NetworkID, &v.Name, &v.StartIP, &v.EndIP, &v.Notes); err != nil {
		return domain.Reservation{}, notFound(fmt.Errorf("scan reservation: %w", err))
	}
	return v, nil
}
