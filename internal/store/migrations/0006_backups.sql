CREATE TABLE backups (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source      TEXT NOT NULL,
    destination TEXT NOT NULL DEFAULT '',
    frequency   TEXT NOT NULL DEFAULT '',
    last_run    TEXT NOT NULL DEFAULT '',
    notes       TEXT NOT NULL DEFAULT ''
);
