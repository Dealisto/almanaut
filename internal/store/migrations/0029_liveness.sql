-- Liveness checks (#90). check_address is an optional host:port probed by the
-- internal job runner; empty means the entity is not monitored. liveness_state
-- holds the latest per-entity result (derived runtime state, not user-edited).
ALTER TABLE hosts    ADD COLUMN check_address TEXT NOT NULL DEFAULT '';
ALTER TABLE services ADD COLUMN check_address TEXT NOT NULL DEFAULT '';

CREATE TABLE liveness_state (
    entity_type TEXT    NOT NULL,          -- 'host' | 'service'
    entity_id   INTEGER NOT NULL,
    status      TEXT    NOT NULL,          -- 'up' | 'down'
    checked_at  TEXT    NOT NULL,          -- RFC3339
    changed_at  TEXT    NOT NULL,          -- RFC3339, advances only on status change
    last_error  TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (entity_type, entity_id)
);
