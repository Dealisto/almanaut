-- Existing credentials retain their no-expiry behavior during upgrades.
ALTER TABLE api_tokens ADD COLUMN expires_at TEXT NOT NULL DEFAULT '';
ALTER TABLE api_tokens ADD COLUMN last_used_at TEXT NOT NULL DEFAULT '';
ALTER TABLE api_tokens ADD COLUMN rotated_from_id INTEGER REFERENCES api_tokens(id) ON DELETE SET NULL;
CREATE UNIQUE INDEX idx_api_tokens_rotation ON api_tokens(rotated_from_id) WHERE rotated_from_id IS NOT NULL;
