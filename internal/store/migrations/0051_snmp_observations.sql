-- Operational observations never overwrite inventory. Keep only the latest
-- successful snapshot and last attempt per explicitly configured source.
CREATE TABLE snmp_observations (
    source_id TEXT PRIMARY KEY,
    config_fingerprint TEXT NOT NULL DEFAULT '',
    observed_at TEXT NOT NULL DEFAULT '',
    attempted_at TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}'
);
