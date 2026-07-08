-- Attachments: files (invoices, configs, photos) attached to any entity,
-- stored as blobs addressed by (entity_type, entity_id) like tags/journal.
CREATE TABLE attachments (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type  TEXT NOT NULL,
    entity_id    INTEGER NOT NULL,
    filename     TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size         INTEGER NOT NULL,
    content      BLOB NOT NULL,
    uploaded_at  TEXT NOT NULL,
    uploaded_by  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_attachments_entity ON attachments(entity_type, entity_id);
