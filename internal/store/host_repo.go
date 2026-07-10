package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

// HostRepo persists Host entities in SQLite.
type HostRepo struct {
	db DBTX
}

// NewHostRepo returns a HostRepo backed by db.
func NewHostRepo(db *sql.DB) *HostRepo {
	return &HostRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *HostRepo) WithTx(tx *sql.Tx) *HostRepo {
	return &HostRepo{db: tx}
}

// DeleteTx removes the host with the given id within tx.
func (r *HostRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}

// CreateTx inserts h within tx and returns its new id.
func (r *HostRepo) CreateTx(tx *sql.Tx, h domain.Host) (int64, error) { return r.WithTx(tx).Create(h) }

// UpdateTx overwrites the host with h.ID within tx.
func (r *HostRepo) UpdateTx(tx *sql.Tx, h domain.Host) error { return r.WithTx(tx).Update(h) }

// GetTx returns the host with the given id within tx.
func (r *HostRepo) GetTx(tx *sql.Tx, id int64) (domain.Host, error) { return r.WithTx(tx).Get(id) }

// Create inserts h and returns its new ID.
func (r *HostRepo) Create(h domain.Host) (int64, error) {
	if h.IPs == nil {
		h.IPs = []string{}
	}
	ips, err := json.Marshal(h.IPs)
	if err != nil {
		return 0, fmt.Errorf("marshal ips: %w", err)
	}
	res, err := r.db.Exec(
		`INSERT INTO hosts (name, type, os, cpu, ram, disk, status, ips, notes, rack_id, rack_position, u_height, check_address)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.Name, h.Type, h.OS, h.CPU, h.RAM, h.Disk, h.Status, string(ips), h.Notes, h.RackID, h.RackPosition, h.UHeight, h.CheckAddress,
	)
	if err != nil {
		return 0, fmt.Errorf("insert host: %w", err)
	}
	return res.LastInsertId()
}

// Update overwrites the host with h.ID with the values in h.
func (r *HostRepo) Update(h domain.Host) error {
	if h.IPs == nil {
		h.IPs = []string{}
	}
	ips, err := json.Marshal(h.IPs)
	if err != nil {
		return fmt.Errorf("marshal ips: %w", err)
	}
	res, err := r.db.Exec(
		`UPDATE hosts SET name=?, type=?, os=?, cpu=?, ram=?, disk=?, status=?, ips=?, notes=?, rack_id=?, rack_position=?, u_height=?, check_address=?
		 WHERE id=?`,
		h.Name, h.Type, h.OS, h.CPU, h.RAM, h.Disk, h.Status, string(ips), h.Notes, h.RackID, h.RackPosition, h.UHeight, h.CheckAddress, h.ID,
	)
	if err != nil {
		return fmt.Errorf("update host: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Get returns the host with the given id.
func (r *HostRepo) Get(id int64) (domain.Host, error) {
	row := r.db.QueryRow(
		`SELECT h.id, h.name, h.type, h.os, h.cpu, h.ram, h.disk, h.status, h.ips, h.notes,
		        h.rack_id, h.rack_position, h.u_height, h.check_address,
		        l.status, l.checked_at, l.changed_at, l.last_error
		 FROM hosts h
		 LEFT JOIN liveness_state l ON l.entity_type = 'host' AND l.entity_id = h.id
		 WHERE h.id = ?`, id,
	)
	return scanHost(row)
}

// List returns all hosts ordered by name.
func (r *HostRepo) List() ([]domain.Host, error) {
	rows, err := r.db.Query(
		`SELECT h.id, h.name, h.type, h.os, h.cpu, h.ram, h.disk, h.status, h.ips, h.notes,
		        h.rack_id, h.rack_position, h.u_height, h.check_address,
		        l.status, l.checked_at, l.changed_at, l.last_error
		 FROM hosts h
		 LEFT JOIN liveness_state l ON l.entity_type = 'host' AND l.entity_id = h.id
		 ORDER BY h.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query hosts: %w", err)
	}
	defer rows.Close()
	hosts := []domain.Host{}
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, rows.Err()
}

// Delete removes the host with the given id.
func (r *HostRepo) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM hosts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete host: %w", err)
	}
	return nil
}

func scanHost(s scanner) (domain.Host, error) {
	var h domain.Host
	var ipsJSON string
	var lStatus, lChecked, lChanged, lErr sql.NullString
	if err := s.Scan(
		&h.ID, &h.Name, &h.Type, &h.OS, &h.CPU, &h.RAM, &h.Disk, &h.Status, &ipsJSON, &h.Notes,
		&h.RackID, &h.RackPosition, &h.UHeight, &h.CheckAddress,
		&lStatus, &lChecked, &lChanged, &lErr,
	); err != nil {
		return domain.Host{}, notFound(fmt.Errorf("scan host: %w", err))
	}
	if err := json.Unmarshal([]byte(ipsJSON), &h.IPs); err != nil {
		return domain.Host{}, fmt.Errorf("unmarshal ips: %w", err)
	}
	h.Liveness = livenessFromNulls(lStatus, lChecked, lChanged, lErr)
	return h, nil
}

// livenessFromNulls builds a *domain.LivenessStatus from joined liveness_state
// columns; returns nil when the LEFT JOIN produced no row.
func livenessFromNulls(status, checked, changed, lastErr sql.NullString) *domain.LivenessStatus {
	if !status.Valid {
		return nil
	}
	ls := &domain.LivenessStatus{Status: status.String, LastError: lastErr.String}
	ls.CheckedAt, _ = time.Parse(time.RFC3339, checked.String)
	ls.ChangedAt, _ = time.Parse(time.RFC3339, changed.String)
	return ls
}
