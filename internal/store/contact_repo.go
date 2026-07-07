package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// ContactRepo persists Contact entities in SQLite.
type ContactRepo struct{ db DBTX }

func NewContactRepo(db *sql.DB) *ContactRepo          { return &ContactRepo{db: db} }
func (r *ContactRepo) WithTx(tx *sql.Tx) *ContactRepo { return &ContactRepo{db: tx} }
func (r *ContactRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}
func (r *ContactRepo) CreateTx(tx *sql.Tx, c domain.Contact) (int64, error) {
	return r.WithTx(tx).Create(c)
}
func (r *ContactRepo) UpdateTx(tx *sql.Tx, c domain.Contact) error { return r.WithTx(tx).Update(c) }
func (r *ContactRepo) GetTx(tx *sql.Tx, id int64) (domain.Contact, error) {
	return r.WithTx(tx).Get(id)
}

func (r *ContactRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM contacts`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count contacts: %w", err)
	}
	return n, nil
}

func (r *ContactRepo) Create(c domain.Contact) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO contacts (name, email, phone, role, organization, notes) VALUES (?, ?, ?, ?, ?, ?)`,
		c.Name, c.Email, c.Phone, c.Role, c.Organization, c.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert contact: %w", err)
	}
	return res.LastInsertId()
}

func (r *ContactRepo) Get(id int64) (domain.Contact, error) {
	row := r.db.QueryRow(`SELECT id, name, email, phone, role, organization, notes FROM contacts WHERE id = ?`, id)
	return scanContact(row)
}

func (r *ContactRepo) List() ([]domain.Contact, error) {
	rows, err := r.db.Query(`SELECT id, name, email, phone, role, organization, notes FROM contacts ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query contacts: %w", err)
	}
	defer rows.Close()
	contacts := []domain.Contact{}
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

func (r *ContactRepo) Update(c domain.Contact) error {
	res, err := r.db.Exec(
		`UPDATE contacts SET name=?, email=?, phone=?, role=?, organization=?, notes=? WHERE id=?`,
		c.Name, c.Email, c.Phone, c.Role, c.Organization, c.Notes, c.ID,
	)
	if err != nil {
		return fmt.Errorf("update contact: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

func (r *ContactRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM contacts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete contact: %w", err)
	}
	return nil
}

func scanContact(s scanner) (domain.Contact, error) {
	var c domain.Contact
	if err := s.Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.Role, &c.Organization, &c.Notes); err != nil {
		return domain.Contact{}, notFound(fmt.Errorf("scan contact: %w", err))
	}
	return c, nil
}
