-- TOTP two-factor authentication (#98). Opt-in per user: user_totp holds the
-- shared secret and whether 2FA is confirmed/enabled; recovery codes are stored
-- hashed and single-use; totp_pending is the short-lived post-password /
-- pre-2FA challenge state. All cascade on user deletion, like sessions/tokens.
CREATE TABLE user_totp (
    user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret     TEXT    NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 0, -- 0 = enrollment pending, 1 = confirmed
    created_at TEXT    NOT NULL
);

CREATE TABLE totp_recovery_codes (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT    NOT NULL,          -- sha256 hex of the normalized code
    used      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_totp_recovery_user ON totp_recovery_codes (user_id);

CREATE TABLE totp_pending (
    token_hash TEXT    PRIMARY KEY,      -- sha256 of the pending-challenge cookie
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT    NOT NULL          -- RFC3339
);
