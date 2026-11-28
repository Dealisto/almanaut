CREATE TABLE network_interfaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_type TEXT NOT NULL,
    owner_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'physical',
    port_id INTEGER NOT NULL DEFAULT 0,
    vlan_id INTEGER NOT NULL DEFAULT 0,
    mac TEXT NOT NULL DEFAULT '',
    mtu INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    notes TEXT NOT NULL DEFAULT '',
    UNIQUE(owner_type, owner_id, name)
);
CREATE INDEX network_interfaces_port ON network_interfaces(port_id);
CREATE TABLE ip_addresses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    address TEXT NOT NULL,
    interface_id INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    dns_name TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT ''
);
CREATE INDEX ip_addresses_interface ON ip_addresses(interface_id);
CREATE INDEX ip_addresses_address ON ip_addresses(address);
