-- Remember the deadline sent, so renewals inside the alert window re-arm.
ALTER TABLE notification_state ADD COLUMN notified_date TEXT NOT NULL DEFAULT '';

-- Preserve existing suppression across upgrades using the current inventory
-- deadline. Older versions did not retain the date originally delivered.
UPDATE notification_state SET notified_date = COALESCE(CASE kind
    WHEN 'certificate' THEN (SELECT expires_on FROM certificates WHERE id = entity_id)
    WHEN 'hardware' THEN (SELECT warranty_end FROM hardware WHERE id = entity_id)
    WHEN 'subscription' THEN (SELECT renewal_date FROM subscriptions WHERE id = entity_id)
END, '');
