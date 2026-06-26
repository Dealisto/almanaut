CREATE TABLE services (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    name     TEXT NOT NULL,
    kind     TEXT NOT NULL,
    url      TEXT NOT NULL DEFAULT '',
    ports    TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    notes    TEXT NOT NULL DEFAULT ''
);
