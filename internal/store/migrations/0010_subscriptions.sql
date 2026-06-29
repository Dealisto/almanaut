CREATE TABLE subscriptions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL,
    kind          TEXT NOT NULL DEFAULT '',
    provider      TEXT NOT NULL DEFAULT '',
    amount        TEXT NOT NULL DEFAULT '',
    currency      TEXT NOT NULL DEFAULT '',
    billing_cycle TEXT NOT NULL DEFAULT '',
    renewal_date  TEXT NOT NULL DEFAULT '',
    auto_renew    INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT ''
);
