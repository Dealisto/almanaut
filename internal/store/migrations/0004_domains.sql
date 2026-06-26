CREATE TABLE domains (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    fqdn     TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT '',
    notes    TEXT NOT NULL DEFAULT ''
);
