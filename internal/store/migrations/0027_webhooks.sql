CREATE TABLE webhooks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    url          TEXT NOT NULL,
    secret       TEXT NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    entity_types TEXT NOT NULL DEFAULT '',  -- comma-separated; empty = all types
    events       TEXT NOT NULL DEFAULT '',  -- comma-separated created,updated,deleted; empty = all
    created_at   TEXT NOT NULL
);
