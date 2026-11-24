-- Successful observations are derived operational state, not inventory data.
-- A replacement binding must not inherit the old resource's last-seen time.
CREATE TABLE discovery_observations (
    source      TEXT NOT NULL,
    external_id TEXT NOT NULL,
    last_seen   TEXT NOT NULL,
    PRIMARY KEY (source, external_id),
    FOREIGN KEY (source, external_id)
        REFERENCES discovery_bindings(source, external_id) ON DELETE CASCADE
);
