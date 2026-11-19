CREATE TABLE host_types (
    name TEXT PRIMARY KEY,
    label TEXT NOT NULL CHECK (length(trim(label)) > 0)
);

INSERT INTO host_types (name, label) VALUES
    ('physical', 'physical'), ('vm', 'vm'), ('lxc', 'lxc'), ('vps', 'vps');

-- Preserve any existing values from older versions or manual imports.
INSERT OR IGNORE INTO host_types (name, label)
SELECT DISTINCT type, type FROM hosts WHERE trim(type) <> '';

CREATE INDEX idx_hosts_type ON hosts(type);
