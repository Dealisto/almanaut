-- IP reservations: named address ranges within a network, honoured by IPAM.
CREATE TABLE ip_reservations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id INTEGER NOT NULL DEFAULT 0,
    name       TEXT NOT NULL,
    start_ip   TEXT NOT NULL DEFAULT '',
    end_ip     TEXT NOT NULL DEFAULT '',
    notes      TEXT NOT NULL DEFAULT ''
);
