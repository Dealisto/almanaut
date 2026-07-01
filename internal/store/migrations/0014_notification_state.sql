-- Tracks which entities have already triggered an expiry notification, so each
-- item notifies at most once until it leaves the "expiring soon" window (is
-- renewed) or is deleted, at which point its row is cleared and future
-- expiries re-arm. kind is the entity class ('certificate' | 'hardware' |
-- 'subscription'); entity_id is that entity's id.
CREATE TABLE notification_state (
    kind        TEXT NOT NULL,
    entity_id   INTEGER NOT NULL,
    notified_at TEXT NOT NULL,
    PRIMARY KEY (kind, entity_id)
);
