package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// ServiceRepo persists Service entities in SQLite.
type ServiceRepo struct {
	db DBTX
}

// NewServiceRepo returns a ServiceRepo backed by db.
func NewServiceRepo(db *sql.DB) *ServiceRepo {
	return &ServiceRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *ServiceRepo) WithTx(tx *sql.Tx) *ServiceRepo {
	return &ServiceRepo{db: tx}
}

// DeleteTx removes the service with the given id within tx.
func (r *ServiceRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}

// CreateTx inserts s within tx and returns its new id.
func (r *ServiceRepo) CreateTx(tx *sql.Tx, s domain.Service) (int64, error) {
	return r.WithTx(tx).Create(s)
}

// UpdateTx overwrites the service with s.ID within tx.
func (r *ServiceRepo) UpdateTx(tx *sql.Tx, s domain.Service) error { return r.WithTx(tx).Update(s) }

// GetTx returns the service with the given id within tx.
func (r *ServiceRepo) GetTx(tx *sql.Tx, id int64) (domain.Service, error) {
	return r.WithTx(tx).Get(id)
}

// Create inserts s and returns its new ID.
func (r *ServiceRepo) Create(s domain.Service) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO services (name, kind, url, ports, category, notes, check_address)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.Name, s.Kind, s.URL, s.Ports, s.Category, s.Notes, s.CheckAddress,
	)
	if err != nil {
		return 0, fmt.Errorf("insert service: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the service with the given id.
func (r *ServiceRepo) Get(id int64) (domain.Service, error) {
	row := r.db.QueryRow(
		`SELECT s.id, s.name, s.kind, s.url, s.ports, s.category, s.notes, s.check_address,
		        l.status, l.checked_at, l.changed_at, l.last_error
		 FROM services s
		 LEFT JOIN liveness_state l ON l.entity_type = 'service' AND l.entity_id = s.id
		 WHERE s.id = ?`, id,
	)
	return scanService(row)
}

// List returns all services ordered by name.
func (r *ServiceRepo) List() ([]domain.Service, error) {
	rows, err := r.db.Query(
		`SELECT s.id, s.name, s.kind, s.url, s.ports, s.category, s.notes, s.check_address,
		        l.status, l.checked_at, l.changed_at, l.last_error
		 FROM services s
		 LEFT JOIN liveness_state l ON l.entity_type = 'service' AND l.entity_id = s.id
		 ORDER BY s.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query services: %w", err)
	}
	defer rows.Close()
	services := []domain.Service{}
	for rows.Next() {
		s, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		services = append(services, s)
	}
	return services, rows.Err()
}

// Update overwrites the service with s.ID with the values in s.
func (r *ServiceRepo) Update(s domain.Service) error {
	res, err := r.db.Exec(
		`UPDATE services SET name=?, kind=?, url=?, ports=?, category=?, notes=?, check_address=? WHERE id=?`,
		s.Name, s.Kind, s.URL, s.Ports, s.Category, s.Notes, s.CheckAddress, s.ID,
	)
	if err != nil {
		return fmt.Errorf("update service: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes the service with the given id.
func (r *ServiceRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM services WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

func scanService(s scanner) (domain.Service, error) {
	var svc domain.Service
	var lStatus, lChecked, lChanged, lErr sql.NullString
	if err := s.Scan(
		&svc.ID, &svc.Name, &svc.Kind, &svc.URL, &svc.Ports, &svc.Category, &svc.Notes, &svc.CheckAddress,
		&lStatus, &lChecked, &lChanged, &lErr,
	); err != nil {
		return domain.Service{}, notFound(fmt.Errorf("scan service: %w", err))
	}
	svc.Liveness = livenessFromNulls(lStatus, lChecked, lChanged, lErr)
	return svc, nil
}
