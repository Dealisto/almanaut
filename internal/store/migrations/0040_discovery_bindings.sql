-- Discovery identities are inventory metadata, independent of editable names.
-- Source contains a stable kind:id key; credentials remain in configuration.
CREATE TABLE discovery_bindings (
    source      TEXT NOT NULL CHECK (length(trim(source)) > 0),
    external_id TEXT NOT NULL CHECK (length(trim(external_id)) > 0),
    entity_type TEXT NOT NULL CHECK (entity_type IN ('host', 'service')),
    entity_id   INTEGER NOT NULL CHECK (entity_id > 0),
    PRIMARY KEY (source, external_id)
);
CREATE INDEX idx_discovery_bindings_entity ON discovery_bindings (entity_type, entity_id);

-- The target is polymorphic, so triggers provide the foreign-key invariant.
CREATE TRIGGER discovery_bindings_target_insert
BEFORE INSERT ON discovery_bindings
WHEN (NEW.entity_type = 'host' AND NOT EXISTS (SELECT 1 FROM hosts WHERE id = NEW.entity_id))
  OR (NEW.entity_type = 'service' AND NOT EXISTS (SELECT 1 FROM services WHERE id = NEW.entity_id))
BEGIN
    SELECT RAISE(ABORT, 'discovery binding target does not exist');
END;
CREATE TRIGGER discovery_bindings_target_update
BEFORE UPDATE OF entity_type, entity_id ON discovery_bindings
WHEN (NEW.entity_type = 'host' AND NOT EXISTS (SELECT 1 FROM hosts WHERE id = NEW.entity_id))
  OR (NEW.entity_type = 'service' AND NOT EXISTS (SELECT 1 FROM services WHERE id = NEW.entity_id))
BEGIN
    SELECT RAISE(ABORT, 'discovery binding target does not exist');
END;
CREATE TRIGGER hosts_delete_discovery_bindings
AFTER DELETE ON hosts
BEGIN
    DELETE FROM discovery_bindings WHERE entity_type = 'host' AND entity_id = OLD.id;
END;
CREATE TRIGGER services_delete_discovery_bindings
AFTER DELETE ON services
BEGIN
    DELETE FROM discovery_bindings WHERE entity_type = 'service' AND entity_id = OLD.id;
END;
