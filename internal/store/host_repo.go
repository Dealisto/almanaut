package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/almanaut/almanaut/internal/domain"
)

// HostRepo persists Host entities in SQLite.
type HostRepo struct {
	db *sql.DB
}

// NewHostRepo returns a HostRepo backed by db.
func NewHostRepo(db *sql.DB) *HostRepo {
	return &HostRepo{db: db}
}

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
		`INSERT INTO hosts (name, type, os, cpu, ram, disk, status, ips, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.Name, h.Type, h.OS, h.CPU, h.RAM, h.Disk, h.Status, string(ips), h.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert host: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the host with the given id.
func (r *HostRepo) Get(id int64) (domain.Host, error) {
	row := r.db.QueryRow(
		`SELECT id, name, type, os, cpu, ram, disk, status, ips, notes
		 FROM hosts WHERE id = ?`, id,
	)
	return scanHost(row)
}

// List returns all hosts ordered by name.
func (r *HostRepo) List() ([]domain.Host, error) {
	rows, err := r.db.Query(
		`SELECT id, name, type, os, cpu, ram, disk, status, ips, notes
		 FROM hosts ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query hosts: %w", err)
	}
	defer rows.Close()
	var hosts []domain.Host
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

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanHost(s scanner) (domain.Host, error) {
	var h domain.Host
	var ipsJSON string
	if err := s.Scan(
		&h.ID, &h.Name, &h.Type, &h.OS, &h.CPU, &h.RAM, &h.Disk, &h.Status, &ipsJSON, &h.Notes,
	); err != nil {
		return domain.Host{}, fmt.Errorf("scan host: %w", err)
	}
	if err := json.Unmarshal([]byte(ipsJSON), &h.IPs); err != nil {
		return domain.Host{}, fmt.Errorf("unmarshal ips: %w", err)
	}
	return h, nil
}
