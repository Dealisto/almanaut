CREATE TABLE hosts (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    name    TEXT NOT NULL,
    type    TEXT NOT NULL,
    os      TEXT NOT NULL DEFAULT '',
    cpu     TEXT NOT NULL DEFAULT '',
    ram     TEXT NOT NULL DEFAULT '',
    disk    TEXT NOT NULL DEFAULT '',
    status  TEXT NOT NULL DEFAULT '',
    ips     TEXT NOT NULL DEFAULT '[]', -- JSON array of strings
    notes   TEXT NOT NULL DEFAULT ''
);
