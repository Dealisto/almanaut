-- Probe observations describe a particular endpoint. Keep ordinary edits from
-- discarding useful state, but never carry an old endpoint's status or TLS
-- identity into the first observation of a replacement endpoint.
CREATE TRIGGER hosts_liveness_state_target_changed AFTER UPDATE OF check_address ON hosts
WHEN OLD.check_address != NEW.check_address BEGIN
    DELETE FROM liveness_state WHERE entity_type = 'host' AND entity_id = OLD.id;
END;
CREATE TRIGGER services_liveness_state_target_changed AFTER UPDATE OF check_address ON services
WHEN OLD.check_address != NEW.check_address BEGIN
    DELETE FROM liveness_state WHERE entity_type = 'service' AND entity_id = OLD.id;
END;
CREATE TRIGGER certificates_probe_state_target_changed AFTER UPDATE OF probe_target ON certificates
WHEN OLD.probe_target != NEW.probe_target BEGIN
    DELETE FROM cert_probe_state WHERE certificate_id = OLD.id;
END;

CREATE TRIGGER hosts_liveness_state_deleted AFTER DELETE ON hosts BEGIN
    DELETE FROM liveness_state WHERE entity_type = 'host' AND entity_id = OLD.id;
END;
CREATE TRIGGER services_liveness_state_deleted AFTER DELETE ON services BEGIN
    DELETE FROM liveness_state WHERE entity_type = 'service' AND entity_id = OLD.id;
END;
CREATE TRIGGER certificates_probe_state_deleted AFTER DELETE ON certificates BEGIN
    DELETE FROM cert_probe_state WHERE certificate_id = OLD.id;
END;

-- Remove orphaned observations left by deletes before these triggers existed.
DELETE FROM liveness_state WHERE entity_type = 'host' AND entity_id NOT IN (SELECT id FROM hosts);
DELETE FROM liveness_state WHERE entity_type = 'service' AND entity_id NOT IN (SELECT id FROM services);
DELETE FROM cert_probe_state WHERE certificate_id NOT IN (SELECT id FROM certificates);
