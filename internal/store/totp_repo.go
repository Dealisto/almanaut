package store

import (
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// TOTPRepo persists per-user TOTP secrets, recovery codes, and the short-lived
// pre-2FA challenge state.
type TOTPRepo struct {
	db DBTX
}

// NewTOTPRepo returns a TOTPRepo backed by db.
func NewTOTPRepo(db *sql.DB) *TOTPRepo { return &TOTPRepo{db: db} }

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *TOTPRepo) WithTx(tx *sql.Tx) *TOTPRepo { return &TOTPRepo{db: tx} }

// SetSecret starts (or restarts) enrollment: it stores a new secret for userID
// with enabled=0, replacing any prior pending or active secret.
func (r *TOTPRepo) SetSecret(userID int64, secret, createdAt string) error {
	if _, err := r.db.Exec(
		`INSERT INTO user_totp (user_id, secret, enabled, created_at) VALUES (?, ?, 0, ?)
		 ON CONFLICT(user_id) DO UPDATE SET secret = excluded.secret, enabled = 0, created_at = excluded.created_at`,
		userID, secret, createdAt,
	); err != nil {
		return fmt.Errorf("set totp secret: %w", err)
	}
	return nil
}

// Get returns userID's TOTP state, or ErrNotFound when 2FA was never set up.
func (r *TOTPRepo) Get(userID int64) (domain.UserTOTP, error) {
	var t domain.UserTOTP
	var enabled int
	err := r.db.QueryRow(
		`SELECT secret, enabled FROM user_totp WHERE user_id = ?`, userID,
	).Scan(&t.Secret, &enabled)
	if err != nil {
		return domain.UserTOTP{}, notFound(fmt.Errorf("get totp: %w", err))
	}
	t.Enabled = enabled == 1
	return t, nil
}

// Enable marks userID's enrollment confirmed. ErrNotFound if no secret exists.
func (r *TOTPRepo) Enable(userID int64) error {
	res, err := r.db.Exec(`UPDATE user_totp SET enabled = 1 WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("enable totp: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Disable removes userID's TOTP secret and all their recovery codes.
func (r *TOTPRepo) Disable(userID int64) error {
	if _, err := r.db.Exec(`DELETE FROM user_totp WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("disable totp: %w", err)
	}
	if _, err := r.db.Exec(`DELETE FROM totp_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete recovery codes: %w", err)
	}
	return nil
}

// ReplaceRecoveryCodes deletes userID's existing recovery codes and stores the
// given hashes. Run inside a transaction (WithTx) so the swap is atomic.
func (r *TOTPRepo) ReplaceRecoveryCodes(userID int64, hashes []string) error {
	if _, err := r.db.Exec(`DELETE FROM totp_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("clear recovery codes: %w", err)
	}
	for _, h := range hashes {
		if _, err := r.db.Exec(
			`INSERT INTO totp_recovery_codes (user_id, code_hash, used) VALUES (?, ?, 0)`,
			userID, h,
		); err != nil {
			return fmt.Errorf("insert recovery code: %w", err)
		}
	}
	return nil
}

// UseRecoveryCode marks an unused recovery code consumed, returning true when a
// matching unused code existed (a single-use redemption).
func (r *TOTPRepo) UseRecoveryCode(userID int64, hash string) (bool, error) {
	res, err := r.db.Exec(
		`UPDATE totp_recovery_codes SET used = 1 WHERE user_id = ? AND code_hash = ? AND used = 0`,
		userID, hash,
	)
	if err != nil {
		return false, fmt.Errorf("use recovery code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountUnusedRecovery returns how many of userID's recovery codes are still
// usable, for display on the account page.
func (r *TOTPRepo) CountUnusedRecovery(userID int64) (int, error) {
	var n int
	if err := r.db.QueryRow(
		`SELECT COUNT(*) FROM totp_recovery_codes WHERE user_id = ? AND used = 0`, userID,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("count recovery codes: %w", err)
	}
	return n, nil
}

// CreatePending stores a pre-2FA challenge token (post-password, pre-code).
func (r *TOTPRepo) CreatePending(tokenHash string, userID int64, expiresAt string) error {
	if _, err := r.db.Exec(
		`INSERT INTO totp_pending (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		tokenHash, userID, expiresAt,
	); err != nil {
		return fmt.Errorf("create pending 2fa: %w", err)
	}
	return nil
}

// PendingUser returns the user id for an unexpired pending challenge token, or
// ErrNotFound when it is missing or expired.
func (r *TOTPRepo) PendingUser(tokenHash, now string) (int64, error) {
	var uid int64
	err := r.db.QueryRow(
		`SELECT user_id FROM totp_pending WHERE token_hash = ? AND expires_at > ?`,
		tokenHash, now,
	).Scan(&uid)
	if err != nil {
		return 0, notFound(fmt.Errorf("pending 2fa: %w", err))
	}
	return uid, nil
}

// DeletePending removes a pending challenge token (after success or on logout).
func (r *TOTPRepo) DeletePending(tokenHash string) error {
	if _, err := r.db.Exec(`DELETE FROM totp_pending WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete pending 2fa: %w", err)
	}
	return nil
}

// DeleteExpiredPending prunes stale pending challenges.
func (r *TOTPRepo) DeleteExpiredPending(now string) error {
	if _, err := r.db.Exec(`DELETE FROM totp_pending WHERE expires_at <= ?`, now); err != nil {
		return fmt.Errorf("prune pending 2fa: %w", err)
	}
	return nil
}
