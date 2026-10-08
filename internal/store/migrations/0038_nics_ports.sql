-- NICs: network interface cards on a host (onboard or expansion), NetBox's
-- Module idea reduced to an optional attribution target for ports.
CREATE TABLE nics (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    host_id INTEGER NOT NULL DEFAULT 0,
    name    TEXT NOT NULL,
    kind    TEXT NOT NULL DEFAULT 'onboard',
    model   TEXT NOT NULL DEFAULT '',
    serial  TEXT NOT NULL DEFAULT '',
    notes   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_nics_host ON nics (host_id);

-- Ports: physical network ports owned by a host (NIC ports) or a hardware
-- item (switch/router ports). Owner is polymorphic (owner_type + owner_id)
-- like tags, so owner, nic_id and peer_port_id are all soft references
-- (0 = none). peer_port_id names the port at the other end of the cable, and
-- the link is stored on both ports: each one's peer_port_id names the other.
CREATE TABLE ports (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_type   TEXT NOT NULL,
    owner_id     INTEGER NOT NULL DEFAULT 0,
    nic_id       INTEGER NOT NULL DEFAULT 0,
    name         TEXT NOT NULL,
    mac          TEXT NOT NULL DEFAULT '',
    mgmt_only    INTEGER NOT NULL DEFAULT 0,
    peer_port_id INTEGER NOT NULL DEFAULT 0,
    notes        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_ports_owner ON ports (owner_type, owner_id);

-- A port has at most one link, so no two ports may name the same peer.
-- Enforce it so a race between two writes cannot cable a port to two others.
CREATE UNIQUE INDEX idx_ports_peer ON ports (peer_port_id) WHERE peer_port_id != 0;
