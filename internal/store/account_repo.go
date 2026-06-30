package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// AccountRepo persists Account entities in SQLite.
type AccountRepo struct {
	db DBTX
}

// NewAccountRepo returns an AccountRepo backed by db.
func NewAccountRepo(db *sql.DB) *AccountRepo {
	return &AccountRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *AccountRepo) WithTx(tx *sql.Tx) *AccountRepo {
	return &AccountRepo{db: tx}
}

// DeleteTx removes the account with the given id within tx.
func (r *AccountRepo) DeleteTx(tx *sql.Tx, id int64) error {
	return r.WithTx(tx).Delete(id)
}

// Count returns the number of accounts.
func (r *AccountRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count accounts: %w", err)
	}
	return n, nil
}

const accountColumns = `id, name, kind, username, password_manager, secret_ref, url, status, notes`

// Create inserts a and returns its new ID.
func (r *AccountRepo) Create(a domain.Account) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO accounts (name, kind, username, password_manager, secret_ref, url, status, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Name, a.Kind, a.Username, a.PasswordManager, a.SecretRef, a.URL, a.Status, a.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("insert account: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the account with the given id.
func (r *AccountRepo) Get(id int64) (domain.Account, error) {
	row := r.db.QueryRow(`SELECT `+accountColumns+` FROM accounts WHERE id = ?`, id)
	return scanAccount(row)
}

// List returns all accounts ordered by name.
func (r *AccountRepo) List() ([]domain.Account, error) {
	rows, err := r.db.Query(`SELECT ` + accountColumns + ` FROM accounts ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query accounts: %w", err)
	}
	defer rows.Close()
	accounts := []domain.Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// Update overwrites the account with a.ID with the values in a.
func (r *AccountRepo) Update(a domain.Account) error {
	_, err := r.db.Exec(
		`UPDATE accounts SET name=?, kind=?, username=?, password_manager=?, secret_ref=?, url=?, status=?, notes=? WHERE id=?`,
		a.Name, a.Kind, a.Username, a.PasswordManager, a.SecretRef, a.URL, a.Status, a.Notes, a.ID,
	)
	if err != nil {
		return fmt.Errorf("update account: %w", err)
	}
	return nil
}

// Delete removes the account with the given id.
func (r *AccountRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM accounts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	return nil
}

func scanAccount(s scanner) (domain.Account, error) {
	var a domain.Account
	if err := s.Scan(&a.ID, &a.Name, &a.Kind, &a.Username, &a.PasswordManager, &a.SecretRef, &a.URL, &a.Status, &a.Notes); err != nil {
		return domain.Account{}, fmt.Errorf("scan account: %w", err)
	}
	return a, nil
}
