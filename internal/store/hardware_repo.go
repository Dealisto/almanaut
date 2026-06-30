package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// HardwareRepo persists Hardware entities in SQLite.
type HardwareRepo struct {
	db DBTX
}

// NewHardwareRepo returns a HardwareRepo backed by db.
func NewHardwareRepo(db *sql.DB) *HardwareRepo {
	return &HardwareRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *HardwareRepo) WithTx(tx *sql.Tx) *HardwareRepo {
	return &HardwareRepo{db: tx}
}

// DeleteTx removes the hardware with the given id within tx.
func (r *HardwareRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}

const hardwareColumns = `id, name, kind, manufacturer, model, serial, location, purchase_date, warranty_end, status, notes`

// Create inserts h and returns its new ID.
func (r *HardwareRepo) Create(h domain.Hardware) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO hardware (name, kind, manufacturer, model, serial, location, purchase_date, warranty_end, status, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.Name, h.Kind, h.Manufacturer, h.Model, h.Serial, h.Location, h.PurchaseDate, h.WarrantyEnd, h.Status, h.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert hardware: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the hardware with the given id.
func (r *HardwareRepo) Get(id int64) (domain.Hardware, error) {
	row := r.db.QueryRow(`SELECT `+hardwareColumns+` FROM hardware WHERE id = ?`, id)
	return scanHardware(row)
}

// List returns all hardware ordered by name.
func (r *HardwareRepo) List() ([]domain.Hardware, error) {
	rows, err := r.db.Query(`SELECT ` + hardwareColumns + ` FROM hardware ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query hardware: %w", err)
	}
	defer rows.Close()
	items := []domain.Hardware{}
	for rows.Next() {
		h, err := scanHardware(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, h)
	}
	return items, rows.Err()
}

// Update overwrites the hardware with h.ID with the values in h.
func (r *HardwareRepo) Update(h domain.Hardware) error {
	res, err := r.db.Exec(
		`UPDATE hardware SET name=?, kind=?, manufacturer=?, model=?, serial=?, location=?, purchase_date=?, warranty_end=?, status=?, notes=? WHERE id=?`,
		h.Name, h.Kind, h.Manufacturer, h.Model, h.Serial, h.Location, h.PurchaseDate, h.WarrantyEnd, h.Status, h.Notes, h.ID,
	)
	if err != nil {
		return fmt.Errorf("update hardware: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes the hardware with the given id.
func (r *HardwareRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM hardware WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete hardware: %w", err)
	}
	return nil
}

func scanHardware(s scanner) (domain.Hardware, error) {
	var h domain.Hardware
	if err := s.Scan(&h.ID, &h.Name, &h.Kind, &h.Manufacturer, &h.Model, &h.Serial, &h.Location, &h.PurchaseDate, &h.WarrantyEnd, &h.Status, &h.Notes); err != nil {
		return domain.Hardware{}, notFound(fmt.Errorf("scan hardware: %w", err))
	}
	return h, nil
}
