package store

import (
	"database/sql"
	"fmt"
	"github.com/Dealisto/almanaut/internal/domain"
)

// InterfaceRepo stores structured network inventory. References are checked inside writes.
type InterfaceRepo struct{ db DBTX }

func NewInterfaceRepo(db *sql.DB) *InterfaceRepo          { return &InterfaceRepo{db: db} }
func (r *InterfaceRepo) WithTx(tx *sql.Tx) *InterfaceRepo { return &InterfaceRepo{db: tx} }
func (r *InterfaceRepo) CreateTx(tx *sql.Tx, v domain.NetworkInterface) (int64, error) {
	return r.WithTx(tx).Create(v)
}
func (r *InterfaceRepo) UpdateTx(tx *sql.Tx, v domain.NetworkInterface) error {
	return r.WithTx(tx).Update(v)
}
func (r *InterfaceRepo) DeleteTx(tx *sql.Tx, id int64) error { return r.WithTx(tx).Delete(id) }
func (r *InterfaceRepo) GetTx(tx *sql.Tx, id int64) (domain.NetworkInterface, error) {
	return r.WithTx(tx).Get(id)
}
func (r *InterfaceRepo) Count() (int, error) {
	var n int
	err := r.db.QueryRow("SELECT COUNT(*) FROM network_interfaces").Scan(&n)
	return n, err
}
func (r *InterfaceRepo) Get(id int64) (domain.NetworkInterface, error) {
	return scanInterface(r.db.QueryRow("SELECT id, owner_type, owner_id, name, kind, port_id, vlan_id, mac, mtu, enabled, notes FROM network_interfaces WHERE id=?", id))
}
func scanInterface(s scanner) (domain.NetworkInterface, error) {
	var v domain.NetworkInterface
	err := s.Scan(&v.ID, &v.OwnerType, &v.OwnerID, &v.Name, &v.Kind, &v.PortID, &v.VLANID, &v.MAC, &v.MTU, &v.Enabled, &v.Notes)
	return v, notFound(err)
}
func (r *InterfaceRepo) List() ([]domain.NetworkInterface, error) {
	rows, err := r.db.Query("SELECT id, owner_type, owner_id, name, kind, port_id, vlan_id, mac, mtu, enabled, notes FROM network_interfaces ORDER BY owner_type, owner_id, name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.NetworkInterface{}
	for rows.Next() {
		v, err := scanInterface(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *InterfaceRepo) Create(v domain.NetworkInterface) (int64, error) {
	if db, ok := r.db.(*sql.DB); ok {
		var id int64
		err := WithTx(db, func(tx *sql.Tx) error { var err error; id, err = r.CreateTx(tx, v); return err })
		return id, err
	}
	if err := r.prepare(&v); err != nil {
		return 0, err
	}
	res, err := r.db.Exec("INSERT INTO network_interfaces (owner_type, owner_id, name, kind, port_id, vlan_id, mac, mtu, enabled, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", v.OwnerType, v.OwnerID, v.Name, v.Kind, v.PortID, v.VLANID, v.MAC, v.MTU, v.Enabled, v.Notes)
	if err != nil {
		return 0, fmt.Errorf("create network_interfaces: %w", err)
	}
	return res.LastInsertId()
}
func (r *InterfaceRepo) Update(v domain.NetworkInterface) error {
	if db, ok := r.db.(*sql.DB); ok {
		return WithTx(db, func(tx *sql.Tx) error { return r.UpdateTx(tx, v) })
	}
	if _, err := r.Get(v.ID); err != nil {
		return err
	}
	if err := r.prepare(&v); err != nil {
		return err
	}
	res, err := r.db.Exec("UPDATE network_interfaces SET owner_type=?, owner_id=?, name=?, kind=?, port_id=?, vlan_id=?, mac=?, mtu=?, enabled=?, notes=? WHERE id=?", v.OwnerType, v.OwnerID, v.Name, v.Kind, v.PortID, v.VLANID, v.MAC, v.MTU, v.Enabled, v.Notes, v.ID)
	if err != nil {
		return fmt.Errorf("update network_interfaces: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}
func (r *InterfaceRepo) Delete(id int64) error {
	if db, ok := r.db.(*sql.DB); ok {
		return WithTx(db, func(tx *sql.Tx) error { return r.DeleteTx(tx, id) })
	}
	var n int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM ip_addresses WHERE interface_id=?", id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return &ConstraintError{Reason: "unassign or delete this interface's IP addresses first"}
	}
	res, err := r.db.Exec("DELETE FROM network_interfaces WHERE id=?", id)
	if err != nil {
		return err
	}
	return rowsAffectedOrNotFound(res)
}

func (r *InterfaceRepo) prepare(v *domain.NetworkInterface) error {
	if err := v.Validate(); err != nil {
		return &ConstraintError{Reason: err.Error()}
	}
	table := "hosts"
	if v.OwnerType == "hardware" {
		table = "hardware"
	}
	if err := requireNetworkReference(r.db, table, v.OwnerID, "owner"); err != nil {
		return err
	}
	if v.VLANID != 0 {
		if err := requireNetworkReference(r.db, "vlans", v.VLANID, "VLAN"); err != nil {
			return err
		}
	}
	if v.PortID != 0 {
		var ownerType string
		var ownerID int64
		err := r.db.QueryRow("SELECT owner_type,owner_id FROM ports WHERE id=?", v.PortID).Scan(&ownerType, &ownerID)
		if err == sql.ErrNoRows {
			return &ConstraintError{Reason: "port does not exist"}
		}
		if err != nil {
			return err
		}
		if ownerType != v.OwnerType || ownerID != v.OwnerID {
			return &ConstraintError{Reason: "port belongs to a different owner"}
		}
	}
	var n int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM network_interfaces WHERE owner_type=? AND owner_id=? AND name=? AND id!=?", v.OwnerType, v.OwnerID, v.Name, v.ID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return &ConstraintError{Reason: "an interface with this name already exists on this owner"}
	}
	return nil
}

// requireNetworkReference only receives constant table names from repository code.
func requireNetworkReference(db DBTX, table string, id int64, label string) error {
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE id=?", id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return &ConstraintError{Reason: label + " does not exist"}
	}
	return nil
}
