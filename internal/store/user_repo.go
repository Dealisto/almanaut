package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// UserRepo persists application login accounts in SQLite.
type UserRepo struct {
	db DBTX
}

// NewUserRepo returns a UserRepo backed by db.
func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *UserRepo) WithTx(tx *sql.Tx) *UserRepo {
	return &UserRepo{db: tx}
}

const userColumns = `id, username, role, password_hash, created_at, updated_at`

// Create inserts u and returns its new ID.
func (r *UserRepo) Create(u domain.User) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO users (username, role, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		u.Username, string(u.Role), u.PasswordHash, u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert user: %w", err)
	}
	return res.LastInsertId()
}

// Get returns the user with the given id.
func (r *UserRepo) Get(id int64) (domain.User, error) {
	row := r.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// GetByUsername returns the user with the given username.
func (r *UserRepo) GetByUsername(username string) (domain.User, error) {
	row := r.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE username = ?`, username)
	return scanUser(row)
}

// List returns all users ordered by username.
func (r *UserRepo) List() ([]domain.User, error) {
	rows, err := r.db.Query(`SELECT ` + userColumns + ` FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()
	users := []domain.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UpdatePassword overwrites the password hash and updated_at of the user with id.
func (r *UserRepo) UpdatePassword(id int64, passwordHash, updatedAt string) error {
	res, err := r.db.Exec(
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, updatedAt, id,
	)
	if err != nil {
		return fmt.Errorf("update user password: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes the user with the given id (its sessions cascade away).
func (r *UserRepo) Delete(id int64) error {
	if _, err := r.db.Exec(`DELETE FROM users WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// Count returns the number of user rows.
func (r *UserRepo) Count() (int, error) {
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

func scanUser(s scanner) (domain.User, error) {
	var u domain.User
	if err := s.Scan(&u.ID, &u.Username, &u.Role, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return domain.User{}, notFound(fmt.Errorf("scan user: %w", err))
	}
	return u, nil
}
