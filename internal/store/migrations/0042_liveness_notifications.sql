-- Durable transition notifications are operational state, not inventory data.
CREATE TABLE liveness_notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('host', 'service')),
    entity_id INTEGER NOT NULL,
    label TEXT NOT NULL,
    address TEXT NOT NULL,
    status TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    observed_at TEXT NOT NULL,
    last_attempt_order INTEGER NOT NULL DEFAULT 0,
    last_attempt_run TEXT NOT NULL DEFAULT ''
);
CREATE INDEX liveness_notifications_attempt ON liveness_notifications(last_attempt_order, id);
CREATE INDEX liveness_notifications_entity ON liveness_notifications(entity_type, entity_id, id);

CREATE TRIGGER hosts_liveness_target_changed AFTER UPDATE OF check_address ON hosts
WHEN OLD.check_address != NEW.check_address BEGIN
    DELETE FROM liveness_notifications WHERE entity_type = 'host' AND entity_id = OLD.id;
END;
CREATE TRIGGER services_liveness_target_changed AFTER UPDATE OF check_address ON services
WHEN OLD.check_address != NEW.check_address BEGIN
    DELETE FROM liveness_notifications WHERE entity_type = 'service' AND entity_id = OLD.id;
END;
CREATE TRIGGER hosts_liveness_deleted AFTER DELETE ON hosts BEGIN
    DELETE FROM liveness_notifications WHERE entity_type = 'host' AND entity_id = OLD.id;
END;
CREATE TRIGGER services_liveness_deleted AFTER DELETE ON services BEGIN
    DELETE FROM liveness_notifications WHERE entity_type = 'service' AND entity_id = OLD.id;
END;
