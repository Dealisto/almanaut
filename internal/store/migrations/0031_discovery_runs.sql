-- Scheduled discovery run history (#91). One row per scheduled run of a source;
-- new_keys holds the identifiers of not-yet-tracked findings that run turned up
-- (used to notify only about newly-appeared items). Not a user-editable entity.
CREATE TABLE discovery_runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source      TEXT    NOT NULL,             -- 'docker' | 'network' | 'proxmox'
    started_at  TEXT    NOT NULL,             -- RFC3339
    finished_at TEXT    NOT NULL,             -- RFC3339
    found_count INTEGER NOT NULL,
    new_count   INTEGER NOT NULL,
    error       TEXT    NOT NULL DEFAULT '',
    new_keys    TEXT    NOT NULL DEFAULT ''   -- JSON array
);
CREATE INDEX idx_discovery_runs_source_id ON discovery_runs (source, id);
