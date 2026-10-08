package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// Session is a server-side web session. TokenHash is the sha256 (hex) of the
// opaque cookie value; the raw token is never stored.
type Session struct {
	ID        int64
	TokenHash string
	UserID    int64
	CreatedAt string
	ExpiresAt string
}

// SessionRepo persists web sessions in SQLite.
type SessionRepo struct {
	db DBTX
}

// NewSessionRepo returns a SessionRepo backed by db.
func NewSessionRepo(db *sql.DB) *SessionRepo {
	return &SessionRepo{db: db}
}

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *SessionRepo) WithTx(tx *sql.Tx) *SessionRepo {
	return &SessionRepo{db: tx}
}

// Create inserts s and returns its new ID.
func (r *SessionRepo) Create(s Session) (int64, error) {
	res, err := r.db.Exec(
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		s.TokenHash, s.UserID, s.CreatedAt, s.ExpiresAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert session: %w", err)
	}
	return res.LastInsertId()
}

// UserByToken returns the user owning a non-expired session with tokenHash.
// now is an RFC3339 timestamp; a session with expires_at <= now is treated as
// absent (ErrNotFound), so the caller need not special-case expiry.
func (r *SessionRepo) UserByToken(tokenHash, now string) (domain.User, error) {
	row := r.db.QueryRow(
		`SELECT u.id, u.username, u.role, u.password_hash, u.created_at, u.updated_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = ? AND s.expires_at > ?`,
		tokenHash, now,
	)
	return scanUser(row)
}

// DeleteByToken removes the session with the given token hash (logout).
func (r *SessionRepo) DeleteByToken(tokenHash string) error {
	if _, err := r.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteByUser removes every session of userID, signing them out everywhere
// (after a password reset or a 2FA reset).
func (r *SessionRepo) DeleteByUser(userID int64) error {
	if _, err := r.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

// DeleteOtherSessions removes every session of userID except the one with
// keepTokenHash, so a user who changes their own password stays signed in
// here while every other session (possibly a stolen one) ends.
func (r *SessionRepo) DeleteOtherSessions(userID int64, keepTokenHash string) error {
	if _, err := r.db.Exec(
		`DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, userID, keepTokenHash,
	); err != nil {
		return fmt.Errorf("delete other sessions: %w", err)
	}
	return nil
}

// DeleteExpired prunes every session whose expires_at is at or before now.
func (r *SessionRepo) DeleteExpired(now string) error {
	if _, err := r.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, now); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}
