package store

import (
	"database/sql"
	"fmt"
	"github.com/Dealisto/almanaut/internal/domain"
)

// IPAddressRepo stores structured network inventory. References are checked inside writes.
type IPAddressRepo struct{ db DBTX }

func NewIPAddressRepo(db *sql.DB) *IPAddressRepo          { return &IPAddressRepo{db: db} }
func (r *IPAddressRepo) WithTx(tx *sql.Tx) *IPAddressRepo { return &IPAddressRepo{db: tx} }
func (r *IPAddressRepo) CreateTx(tx *sql.Tx, v domain.IPAddress) (int64, error) {
	return r.WithTx(tx).Create(v)
}
func (r *IPAddressRepo) UpdateTx(tx *sql.Tx, v domain.IPAddress) error { return r.WithTx(tx).Update(v) }
func (r *IPAddressRepo) DeleteTx(tx *sql.Tx, id int64) error           { return r.WithTx(tx).Delete(id) }
func (r *IPAddressRepo) GetTx(tx *sql.Tx, id int64) (domain.IPAddress, error) {
	return r.WithTx(tx).Get(id)
}
func (r *IPAddressRepo) Count() (int, error) {
	var n int
	err := r.db.QueryRow("SELECT COUNT(*) FROM ip_addresses").Scan(&n)
	return n, err
}
func (r *IPAddressRepo) Get(id int64) (domain.IPAddress, error) {
	return scanIPAddress(r.db.QueryRow("SELECT id, address, interface_id, status, dns_name, notes FROM ip_addresses WHERE id=?", id))
}
func scanIPAddress(s scanner) (domain.IPAddress, error) {
	var v domain.IPAddress
	err := s.Scan(&v.ID, &v.Address, &v.InterfaceID, &v.Status, &v.DNSName, &v.Notes)
	return v, notFound(err)
}
func (r *IPAddressRepo) List() ([]domain.IPAddress, error) {
	rows, err := r.db.Query("SELECT id, address, interface_id, status, dns_name, notes FROM ip_addresses ORDER BY address")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.IPAddress{}
	for rows.Next() {
		v, err := scanIPAddress(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *IPAddressRepo) Create(v domain.IPAddress) (int64, error) {
	if db, ok := r.db.(*sql.DB); ok {
		var id int64
		err := WithTx(db, func(tx *sql.Tx) error { var err error; id, err = r.CreateTx(tx, v); return err })
		return id, err
	}
	if err := r.prepare(&v); err != nil {
		return 0, err
	}
	res, err := r.db.Exec("INSERT INTO ip_addresses (address, interface_id, status, dns_name, notes) VALUES (?, ?, ?, ?, ?)", v.Address, v.InterfaceID, v.Status, v.DNSName, v.Notes)
	if err != nil {
		return 0, fmt.Errorf("create ip_addresses: %w", err)
	}
	return res.LastInsertId()
}
func (r *IPAddressRepo) Update(v domain.IPAddress) error {
	if db, ok := r.db.(*sql.DB); ok {
		return WithTx(db, func(tx *sql.Tx) error { return r.UpdateTx(tx, v) })
	}
	if _, err := r.Get(v.ID); err != nil {
		return err
	}
	if err := r.prepare(&v); err != nil {
		return err
	}
	res, err := r.db.Exec("UPDATE ip_addresses SET address=?, interface_id=?, status=?, dns_name=?, notes=? WHERE id=?", v.Address, v.InterfaceID, v.Status, v.DNSName, v.Notes, v.ID)
	if err != nil {
		return fmt.Errorf("update ip_addresses: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}
func (r *IPAddressRepo) Delete(id int64) error {
	if db, ok := r.db.(*sql.DB); ok {
		return WithTx(db, func(tx *sql.Tx) error { return r.DeleteTx(tx, id) })
	}
	res, err := r.db.Exec("DELETE FROM ip_addresses WHERE id=?", id)
	if err != nil {
		return err
	}
	return rowsAffectedOrNotFound(res)
}

func (r *IPAddressRepo) prepare(v *domain.IPAddress) error {
	if err := v.Validate(); err != nil {
		return &ConstraintError{Reason: err.Error()}
	}
	prefix, _ := domain.ParseInterfaceAddress(v.Address)
	v.Address = prefix.String()
	if v.InterfaceID != 0 {
		return requireNetworkReference(r.db, "network_interfaces", v.InterfaceID, "interface")
	}
	return nil
}
