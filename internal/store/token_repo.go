package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

// APIToken is a per-user API credential. TokenHash is the sha256 (hex) of the
// opaque token; the raw token is shown once at creation and never stored.
type APIToken struct {
	ID            int64
	TokenHash     string
	UserID        int64
	Label         string
	Scope         string
	CreatedAt     string
	ExpiresAt     string
	LastUsedAt    string
	RotatedFromID int64
}

func (t APIToken) Expired() bool {
	if t.ExpiresAt == "" {
		return false
	}
	expires, err := time.Parse(time.RFC3339, t.ExpiresAt)
	return err != nil || !time.Now().Before(expires)
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
	if t.ExpiresAt != "" {
		expires, err := time.Parse(time.RFC3339, t.ExpiresAt)
		if err != nil {
			return 0, fmt.Errorf("invalid token expiry: %w", err)
		}
		t.ExpiresAt = expires.UTC().Format(time.RFC3339)
	}
	res, err := r.db.Exec(
		`INSERT INTO api_tokens (token_hash, user_id, label, scope, created_at, expires_at, rotated_from_id) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, 0))`,
		t.TokenHash, t.UserID, t.Label, t.Scope, t.CreatedAt, t.ExpiresAt, t.RotatedFromID,
	)
	if err != nil {
		return 0, fmt.Errorf("insert api token: %w", err)
	}
	return res.LastInsertId()
}

// ListByUser returns userID's tokens, newest first.
func (r *TokenRepo) ListByUser(userID int64) ([]APIToken, error) {
	rows, err := r.db.Query(
		`SELECT id, token_hash, user_id, label, scope, created_at, expires_at, last_used_at, COALESCE(rotated_from_id, 0)
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
		if err := rows.Scan(&t.ID, &t.TokenHash, &t.UserID, &t.Label, &t.Scope, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.RotatedFromID); err != nil {
			return nil, fmt.Errorf("scan api token: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// UserByToken returns the user owning the token with tokenHash and the token's
// scope, or ErrNotFound.
func (r *TokenRepo) UserByToken(tokenHash string) (domain.User, string, error) {
	return r.userByTokenAt(tokenHash, time.Now().UTC())
}

func (r *TokenRepo) userByTokenAt(tokenHash string, now time.Time) (domain.User, string, error) {
	row := r.db.QueryRow(
		`SELECT u.id, u.username, u.role, u.password_hash, u.created_at, u.updated_at, t.scope, t.last_used_at
		 FROM api_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = ? AND (t.expires_at = '' OR julianday(t.expires_at) > julianday(?))`,
		tokenHash, now.Format(time.RFC3339),
	)
	var u domain.User
	var scope string
	var lastUsed string
	if err := row.Scan(&u.ID, &u.Username, &u.Role, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt, &scope, &lastUsed); err != nil {
		return domain.User{}, "", notFound(fmt.Errorf("scan token user: %w", err))
	}
	// Most API calls remain read-only. Once per five minutes, a conditional
	// update records coarse usage; concurrent requests cannot write it twice.
	last, err := time.Parse(time.RFC3339, lastUsed)
	if err != nil || now.Sub(last) >= 5*time.Minute {
		if _, err := r.db.Exec(`UPDATE api_tokens SET last_used_at = ? WHERE token_hash = ? AND last_used_at = ?`, now.Format(time.RFC3339), tokenHash, lastUsed); err != nil {
			return domain.User{}, "", fmt.Errorf("record token use: %w", err)
		}
	}
	return u, scope, nil
}

// GetByOwner deliberately conceals tokens belonging to another user.
func (r *TokenRepo) GetByOwner(id, userID int64) (APIToken, error) {
	var t APIToken
	err := r.db.QueryRow(`SELECT id, token_hash, user_id, label, scope, created_at, expires_at, last_used_at, COALESCE(rotated_from_id, 0) FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&t.ID, &t.TokenHash, &t.UserID, &t.Label, &t.Scope, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.RotatedFromID)
	return t, notFound(err)
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
