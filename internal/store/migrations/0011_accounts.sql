CREATE TABLE accounts (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT NOT NULL,
    kind             TEXT NOT NULL DEFAULT '',
    username         TEXT NOT NULL DEFAULT '',
    password_manager TEXT NOT NULL DEFAULT '',
    secret_ref       TEXT NOT NULL DEFAULT '',
    url              TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT '',
    notes            TEXT NOT NULL DEFAULT ''
);
