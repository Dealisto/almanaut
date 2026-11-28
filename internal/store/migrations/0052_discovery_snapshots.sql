-- Operational source observations; intentionally excluded from inventory exports.
CREATE TABLE discovery_snapshots (
    source TEXT PRIMARY KEY NOT NULL,
    attempted_at INTEGER NOT NULL,
    succeeded_at INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    candidates TEXT NOT NULL DEFAULT '[]'
);
