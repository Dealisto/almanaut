-- Contacts: people or vendors responsible for infrastructure.
CREATE TABLE contacts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    email        TEXT NOT NULL DEFAULT '',
    phone        TEXT NOT NULL DEFAULT '',
    role         TEXT NOT NULL DEFAULT '',
    organization TEXT NOT NULL DEFAULT '',
    notes        TEXT NOT NULL DEFAULT ''
);
