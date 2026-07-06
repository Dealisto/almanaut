-- Application login accounts, distinct from the Account CMDB entity (which
-- tracks inventory credentials). password_hash is a bcrypt hash; the app never
-- stores plaintext. Seeded with an initial admin at first startup.
CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

-- Server-side web sessions. token_hash is the sha256 (hex) of the opaque cookie
-- value; the raw token is never persisted. Sessions cascade-delete with their
-- user (foreign_keys is enabled in store.Open) and are pruned once expired.
CREATE TABLE sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT NOT NULL UNIQUE,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX idx_sessions_expires ON sessions (expires_at);
