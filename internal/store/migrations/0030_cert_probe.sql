-- TLS certificate probing (#89). probe_target is an optional host:port probed
-- by the internal job runner / the "Probe now" button; empty = not probeable.
-- cert_probe_state holds the latest per-certificate probe result (derived state).
ALTER TABLE certificates ADD COLUMN probe_target TEXT NOT NULL DEFAULT '';

CREATE TABLE cert_probe_state (
    certificate_id INTEGER PRIMARY KEY,
    probed_at   TEXT    NOT NULL,          -- RFC3339
    success     INTEGER NOT NULL,          -- 0 | 1
    last_error  TEXT    NOT NULL DEFAULT '',
    serial      TEXT    NOT NULL DEFAULT '',
    issuer      TEXT    NOT NULL DEFAULT '',
    sans        TEXT    NOT NULL DEFAULT '',   -- JSON array
    not_after   TEXT    NOT NULL DEFAULT '',   -- YYYY-MM-DD
    mismatches  TEXT    NOT NULL DEFAULT ''    -- JSON array
);
