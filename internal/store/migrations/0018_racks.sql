CREATE TABLE sites (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    name    TEXT NOT NULL,
    address TEXT NOT NULL DEFAULT '',
    notes   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE locations (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    name    TEXT NOT NULL,
    site_id INTEGER NOT NULL DEFAULT 0,
    notes   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE racks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    location_id INTEGER NOT NULL DEFAULT 0,
    u_height    INTEGER NOT NULL DEFAULT 42,
    notes       TEXT NOT NULL DEFAULT ''
);
