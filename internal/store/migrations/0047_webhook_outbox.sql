-- Immutable deliveries are committed with inventory mutations, then claimed
-- with expiring leases. No endpoint secret is copied into this table.
CREATE TABLE webhook_outbox (
    delivery_id TEXT PRIMARY KEY,
    webhook_id INTEGER NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    body BLOB NOT NULL,
    signature TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    due_at INTEGER NOT NULL,
    lease_until INTEGER NOT NULL DEFAULT 0,
    lease_token TEXT NOT NULL DEFAULT ''
);
CREATE INDEX webhook_outbox_due ON webhook_outbox(due_at, lease_until);
CREATE INDEX webhook_outbox_endpoint ON webhook_outbox(webhook_id);

-- Disabling then re-enabling a receiver must not revive its old deliveries.
-- Changes invalidate pending and leased rows; already-sent HTTP requests may
-- still complete, but cannot schedule another attempt after invalidation.
CREATE TRIGGER webhook_outbox_invalidate
AFTER UPDATE OF url, secret, enabled, entity_types, events ON webhooks
WHEN OLD.url != NEW.url OR OLD.secret != NEW.secret OR OLD.enabled != NEW.enabled
    OR OLD.entity_types != NEW.entity_types OR OLD.events != NEW.events
BEGIN
    DELETE FROM webhook_outbox WHERE webhook_id = OLD.id;
END;
