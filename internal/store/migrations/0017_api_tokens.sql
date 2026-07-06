-- Per-user API tokens for the read-write JSON API. token_hash is the sha256
-- (hex) of the opaque token; the raw token is shown once at creation and never
-- stored. Tokens do not expire (revoke-only) and cascade-delete with their user
-- (foreign_keys is enabled in store.Open).
CREATE TABLE api_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT NOT NULL UNIQUE,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label      TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_api_tokens_user ON api_tokens (user_id);
