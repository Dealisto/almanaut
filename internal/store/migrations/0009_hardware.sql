CREATE TABLE hardware (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL,
    kind          TEXT NOT NULL DEFAULT '',
    manufacturer  TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT '',
    serial        TEXT NOT NULL DEFAULT '',
    location      TEXT NOT NULL DEFAULT '',
    purchase_date TEXT NOT NULL DEFAULT '',
    warranty_end  TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT ''
);
