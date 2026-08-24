package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// NICRepo persists NIC entities in SQLite.
type NICRepo struct{ db DBTX }

func NewNICRepo(db *sql.DB) *NICRepo          { return &NICRepo{db: db} }
func (r *NICRepo) WithTx(tx *sql.Tx) *NICRepo { return &NICRepo{db: tx} }
func (r *NICRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}
func (r *NICRepo) CreateTx(tx *sql.Tx, v domain.NIC) (int64, error) {
	return r.WithTx(tx).Create(v)
}
func (r *NICRepo) UpdateTx(tx *sql.Tx, v domain.NIC) error {
	return r.WithTx(tx).Update(v)
}
func (r *NICRepo) GetTx(tx *sql.Tx, id int64) (domain.NIC, error) {
	return r.WithTx(tx).Get(id)
}

func (r *NICRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM nics`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count nics: %w", err)
	}
	return n, nil
}

func (r *NICRepo) Create(v domain.NIC) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO nics (host_id, name, kind, model, serial, notes) VALUES (?, ?, ?, ?, ?, ?)`,
		v.HostID, v.Name, v.Kind, v.Model, v.Serial, v.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert nic: %w", err)
	}
	return res.LastInsertId()
}

func (r *NICRepo) Get(id int64) (domain.NIC, error) {
	row := r.db.QueryRow(`SELECT id, host_id, name, kind, model, serial, notes FROM nics WHERE id = ?`, id)
	v, err := scanNIC(row)
	if err != nil {
		return domain.NIC{}, err
	}
	decorated, err := r.decorate([]domain.NIC{v})
	if err != nil {
		return domain.NIC{}, err
	}
	return decorated[0], nil
}

func (r *NICRepo) List() ([]domain.NIC, error) {
	rows, err := r.db.Query(`SELECT id, host_id, name, kind, model, serial, notes FROM nics ORDER BY host_id, name`)
	if err != nil {
		return nil, fmt.Errorf("query nics: %w", err)
	}
	defer rows.Close()
	items := []domain.NIC{}
	for rows.Next() {
		v, err := scanNIC(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return r.decorate(items)
}

func (r *NICRepo) Update(v domain.NIC) error {
	res, err := r.db.Exec(
		`UPDATE nics SET host_id=?, name=?, kind=?, model=?, serial=?, notes=? WHERE id=?`,
		v.HostID, v.Name, v.Kind, v.Model, v.Serial, v.Notes, v.ID,
	)
	if err != nil {
		return fmt.Errorf("update nic: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes the NIC and clears the attribution mark on any port that
// referenced it; the ports themselves survive (soft references throughout).
func (r *NICRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`UPDATE ports SET nic_id = 0 WHERE nic_id = ?`, id); err != nil {
		return fmt.Errorf("clear port nic refs: %w", err)
	}
	if _, err := r.db.Exec(`DELETE FROM nics WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete nic: %w", err)
	}
	return nil
}

// decorate fills the derived HostName on each NIC ("host:<id> (deleted)" when
// the referenced host no longer exists).
func (r *NICRepo) decorate(items []domain.NIC) ([]domain.NIC, error) {
	if len(items) == 0 {
		return items, nil
	}
	hosts, err := entityNames(r.db, "hosts")
	if err != nil {
		return nil, err
	}
	for i := range items {
		name, ok := hosts[items[i].HostID]
		if !ok {
			name = fmt.Sprintf("host:%d (deleted)", items[i].HostID)
		}
		items[i].HostName = name
	}
	return items, nil
}

// entityNames loads an id → name map from a table with (id, name) columns.
func entityNames(db DBTX, table string) (map[int64]string, error) {
	rows, err := db.Query(`SELECT id, name FROM ` + table)
	if err != nil {
		return nil, fmt.Errorf("query %s names: %w", table, err)
	}
	defer rows.Close()
	names := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("scan %s name: %w", table, err)
		}
		names[id] = name
	}
	return names, rows.Err()
}

func scanNIC(s scanner) (domain.NIC, error) {
	var v domain.NIC
	if err := s.Scan(&v.ID, &v.HostID, &v.Name, &v.Kind, &v.Model, &v.Serial, &v.Notes); err != nil {
		return domain.NIC{}, notFound(fmt.Errorf("scan nic: %w", err))
	}
	return v, nil
}
