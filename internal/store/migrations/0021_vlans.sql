-- VLANs as first-class entities; networks reference a VLAN by id (0 = none).
CREATE TABLE vlans (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    name  TEXT NOT NULL,
    vid   INTEGER NOT NULL DEFAULT 0,
    notes TEXT NOT NULL DEFAULT ''
);

ALTER TABLE networks ADD COLUMN vlan_id INTEGER NOT NULL DEFAULT 0;

-- Migrate free-text VLANs that are plain integers in 1..4094 into VLAN entities
-- (one per distinct VLAN id), then link each network to its new VLAN.
INSERT INTO vlans (name, vid)
    SELECT 'VLAN ' || CAST(vlan AS INTEGER), CAST(vlan AS INTEGER)
    FROM networks
    WHERE vlan != '' AND vlan NOT GLOB '*[^0-9]*'
      AND CAST(vlan AS INTEGER) BETWEEN 1 AND 4094
    GROUP BY CAST(vlan AS INTEGER);

UPDATE networks
    SET vlan_id = (SELECT id FROM vlans WHERE vlans.vid = CAST(networks.vlan AS INTEGER))
    WHERE vlan != '' AND vlan NOT GLOB '*[^0-9]*'
      AND CAST(vlan AS INTEGER) BETWEEN 1 AND 4094;

-- Preserve any VLAN text that could not become an entity (non-integer or out of
-- range) in the network's notes so nothing is lost, before dropping the column.
UPDATE networks
    SET notes = CASE WHEN notes = '' THEN 'VLAN: ' || vlan
                     ELSE notes || char(10) || 'VLAN: ' || vlan END
    WHERE vlan != '' AND vlan_id = 0;

ALTER TABLE networks DROP COLUMN vlan;
