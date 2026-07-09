package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// APIToken is a per-user API credential. TokenHash is the sha256 (hex) of the
// opaque token; the raw token is shown once at creation and never stored.
type APIToken struct {
	ID        int64
	TokenHash string
	UserID    int64
	Label     string
	Scope     string
	CreatedAt string
}

// TokenRepo persists API tokens in SQLite.
type TokenRepo struct {
	db DBTX
}

// NewTokenRepo returns a TokenRepo backed by db.
func NewTokenRepo(db *sql.DB) *TokenRepo { return &TokenRepo{db: db} }

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *TokenRepo) WithTx(tx *sql.Tx) *TokenRepo { return &TokenRepo{db: tx} }

// Create inserts t and returns its new ID.
func (r *TokenRepo) Create(t APIToken) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO api_tokens (token_hash, user_id, label, scope, created_at) VALUES (?, ?, ?, ?, ?)`,
		t.TokenHash, t.UserID, t.Label, t.Scope, t.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert api token: %w", err)
	}
	return res.LastInsertId()
}

// ListByUser returns userID's tokens, newest first.
func (r *TokenRepo) ListByUser(userID int64) ([]APIToken, error) {
	rows, err := r.db.Query(
		`SELECT id, token_hash, user_id, label, scope, created_at
		 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query api tokens: %w", err)
	}
	defer rows.Close()
	tokens := []APIToken{}
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.TokenHash, &t.UserID, &t.Label, &t.Scope, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan api token: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// UserByToken returns the user owning the token with tokenHash and the token's
// scope, or ErrNotFound.
func (r *TokenRepo) UserByToken(tokenHash string) (domain.User, string, error) {
	row := r.db.QueryRow(
		`SELECT u.id, u.username, u.role, u.password_hash, u.created_at, u.updated_at, t.scope
		 FROM api_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = ?`,
		tokenHash,
	)
	var u domain.User
	var scope string
	if err := row.Scan(&u.ID, &u.Username, &u.Role, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt, &scope); err != nil {
		return domain.User{}, "", notFound(fmt.Errorf("scan token user: %w", err))
	}
	return u, scope, nil
}

// Delete removes token id, but only when it belongs to userID, so a user cannot
// revoke another user's token. A no-op delete reports ErrNotFound.
func (r *TokenRepo) Delete(id, userID int64) error {
	res, err := r.db.Exec(`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete api token: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}
