package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// NetworkRepo persists Network entities in SQLite.
type NetworkRepo struct {
	db DBTX
}

// NewNetworkRepo returns a NetworkRepo backed by db.
func NewNetworkRepo(db *sql.DB) *NetworkRepo {
	return &NetworkRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *NetworkRepo) WithTx(tx *sql.Tx) *NetworkRepo {
	return &NetworkRepo{db: tx}
}

// DeleteTx removes the network with the given id within tx.
func (r *NetworkRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}

// Create inserts n and returns its new ID.
func (r *NetworkRepo) Create(n domain.Network) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO networks (name, cidr, vlan, gateway, notes) VALUES (?, ?, ?, ?, ?)`,
		n.Name, n.CIDR, n.VLAN, n.Gateway, n.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert network: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the network with the given id.
func (r *NetworkRepo) Get(id int64) (domain.Network, error) {
	row := r.db.QueryRow(
		`SELECT id, name, cidr, vlan, gateway, notes FROM networks WHERE id = ?`, id,
	)
	return scanNetwork(row)
}

// List returns all networks ordered by name.
func (r *NetworkRepo) List() ([]domain.Network, error) {
	rows, err := r.db.Query(
		`SELECT id, name, cidr, vlan, gateway, notes FROM networks ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query networks: %w", err)
	}
	defer rows.Close()
	networks := []domain.Network{}
	for rows.Next() {
		n, err := scanNetwork(rows)
		if err != nil {
			return nil, err
		}
		networks = append(networks, n)
	}
	return networks, rows.Err()
}

// Update overwrites the network with n.ID with the values in n.
func (r *NetworkRepo) Update(n domain.Network) error {
	_, err := r.db.Exec(
		`UPDATE networks SET name=?, cidr=?, vlan=?, gateway=?, notes=? WHERE id=?`,
		n.Name, n.CIDR, n.VLAN, n.Gateway, n.Notes, n.ID,
	)
	if err != nil {
		return fmt.Errorf("update network: %w", err)
	}
	return nil
}

// Delete removes the network with the given id.
func (r *NetworkRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM networks WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete network: %w", err)
	}
	return nil
}

func scanNetwork(s scanner) (domain.Network, error) {
	var n domain.Network
	if err := s.Scan(&n.ID, &n.Name, &n.CIDR, &n.VLAN, &n.Gateway, &n.Notes); err != nil {
		return domain.Network{}, fmt.Errorf("scan network: %w", err)
	}
	return n, nil
}
