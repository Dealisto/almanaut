CREATE TABLE certificates (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    subject    TEXT NOT NULL,
    issuer     TEXT NOT NULL DEFAULT '',
    expires_on TEXT NOT NULL,
    auto_renew INTEGER NOT NULL DEFAULT 0,
    notes      TEXT NOT NULL DEFAULT ''
);
