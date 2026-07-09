-- Per-token scope. A token's effective permission is the intersection of this
-- scope and its owner's role. Existing tokens backfill to 'read-write' so
-- upgrades never silently revoke an API client's write access.
ALTER TABLE api_tokens ADD COLUMN scope TEXT NOT NULL DEFAULT 'read-write';
