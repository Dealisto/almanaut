package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
)

// HostTypeRepo manages the type catalog. Mutations run in the caller's
// transaction so membership checks and host reassignment cannot race writes.
type HostTypeRepo struct{ db DBTX }

func NewHostTypeRepo(db *sql.DB) *HostTypeRepo          { return &HostTypeRepo{db: db} }
func (r *HostTypeRepo) WithTx(tx *sql.Tx) *HostTypeRepo { return &HostTypeRepo{db: tx} }

func (r *HostTypeRepo) List() ([]domain.HostType, error) {
	rows, err := r.db.Query(`SELECT name, label FROM host_types ORDER BY CASE name WHEN 'physical' THEN 0 WHEN 'vm' THEN 1 WHEN 'lxc' THEN 2 WHEN 'vps' THEN 3 ELSE 4 END, label COLLATE NOCASE, name`)
	if err != nil {
		return nil, fmt.Errorf("list host types: %w", err)
	}
	defer rows.Close()
	out := []domain.HostType{}
	for rows.Next() {
		var t domain.HostType
		if err := rows.Scan(&t.Name, &t.Label); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *HostTypeRepo) Get(name string) (domain.HostType, error) {
	var t domain.HostType
	err := r.db.QueryRow(`SELECT name, label FROM host_types WHERE name=?`, name).Scan(&t.Name, &t.Label)
	return t, notFound(err)
}

func (r *HostTypeRepo) Require(name string) error {
	_, err := r.Get(name)
	if errors.Is(err, ErrNotFound) {
		return &ConstraintError{Reason: fmt.Sprintf("unknown host type %q; add it in Host types first", name)}
	}
	return err
}

func (r *HostTypeRepo) Create(t domain.HostType) error {
	if db, ok := r.db.(*sql.DB); ok {
		return WithTx(db, func(tx *sql.Tx) error { return r.WithTx(tx).Create(t) })
	}
	t.Label = strings.TrimSpace(t.Label)
	if err := t.Validate(); err != nil {
		return &ConstraintError{Reason: err.Error()}
	}
	if _, err := r.Get(t.Name); err == nil {
		return &ConstraintError{Reason: fmt.Sprintf("host type %q already exists", t.Name)}
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err := r.db.Exec(`INSERT INTO host_types (name, label) VALUES (?, ?)`, t.Name, t.Label)
	return err
}

func (r *HostTypeRepo) UpdateLabel(name, label string) error {
	t := domain.HostType{Name: name, Label: strings.TrimSpace(label)}
	if err := t.Validate(); err != nil {
		return &ConstraintError{Reason: err.Error()}
	}
	res, err := r.db.Exec(`UPDATE host_types SET label=? WHERE name=?`, t.Label, name)
	if err != nil {
		return err
	}
	return rowsAffectedOrNotFound(res)
}

// Delete refuses built-ins and types still in use. Hosts must be reassigned
// inside the same transaction before deleting an in-use custom type.
func (r *HostTypeRepo) Delete(name string) error {
	if db, ok := r.db.(*sql.DB); ok {
		return WithTx(db, func(tx *sql.Tx) error { return r.WithTx(tx).Delete(name) })
	}
	t, err := r.Get(name)
	if err != nil {
		return err
	}
	if t.Builtin() {
		return &ConstraintError{Reason: "built-in host types cannot be deleted"}
	}
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM hosts WHERE type=?`, name).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return &ConstraintError{Reason: fmt.Sprintf("%d hosts use this type; choose a replacement before deleting it", n)}
	}
	_, err = r.db.Exec(`DELETE FROM host_types WHERE name=?`, name)
	return err
}
