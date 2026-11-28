-- Existing hosts retain automatic synchronization until explicitly protected.
ALTER TABLE hosts ADD COLUMN agent_protected_fields TEXT NOT NULL DEFAULT '[]';
