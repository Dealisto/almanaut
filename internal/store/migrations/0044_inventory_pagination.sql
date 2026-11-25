-- Stable page traversal without sorting an entire collection in memory.
CREATE INDEX idx_hosts_page ON hosts(name, id);
CREATE INDEX idx_services_page ON services(name, id);
CREATE INDEX idx_networks_page ON networks(name, id);
CREATE INDEX idx_domains_page ON domains(fqdn, id);
CREATE INDEX idx_certificates_page ON certificates(expires_on, id);
CREATE INDEX idx_backups_page ON backups(source, id);
CREATE INDEX idx_hardware_page ON hardware(name, id);
CREATE INDEX idx_subscriptions_page ON subscriptions(name, id);
CREATE INDEX idx_accounts_page ON accounts(name, id);
CREATE INDEX idx_sites_page ON sites(name, id);
CREATE INDEX idx_locations_page ON locations(name, id);
CREATE INDEX idx_racks_page ON racks(name, id);
CREATE INDEX idx_contacts_page ON contacts(name, id);
CREATE INDEX idx_vlans_page ON vlans(vid, id);
CREATE INDEX idx_reservations_page ON ip_reservations(name, id);
CREATE INDEX idx_nics_page ON nics(host_id, name, id);
CREATE INDEX idx_ports_page ON ports(owner_type, owner_id, length(name), name, id);
CREATE INDEX idx_tags_type_name_entity ON tags(entity_type, name, entity_id);
