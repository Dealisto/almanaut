CREATE TABLE relationships (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    from_type TEXT NOT NULL,
    from_id   INTEGER NOT NULL,
    to_type   TEXT NOT NULL,
    to_id     INTEGER NOT NULL,
    kind      TEXT NOT NULL
);
