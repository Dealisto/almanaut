CREATE TABLE networks (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    name    TEXT NOT NULL,
    cidr    TEXT NOT NULL,
    vlan    TEXT NOT NULL DEFAULT '',
    gateway TEXT NOT NULL DEFAULT '',
    notes   TEXT NOT NULL DEFAULT ''
);
