-- Append-only audit trail of entity create/update/delete (and inventory
-- import), written atomically with the change by the generic CRUD handlers.
-- Rows are never deleted: a delete action is itself a row, so the global
-- activity feed still shows what was removed. label freezes the entity's
-- display name at event time so a since-deleted entity can still be named.
-- changes holds a JSON array of {field, old, new} for updates (the initial
-- values for a create, with empty old), and is empty for a delete.
CREATE TABLE changelog (
    id          INTEGER PRIMARY KEY,
    entity_type TEXT NOT NULL,
    entity_id   INTEGER NOT NULL,
    label       TEXT NOT NULL,
    action      TEXT NOT NULL,
    actor       TEXT NOT NULL DEFAULT '',
    changes     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);
CREATE INDEX idx_changelog_entity ON changelog (entity_type, entity_id);

-- Manual, categorised journal entries attached to a live entity. Removed with
-- the entity (like tags/relationships). kind is one of
-- info|success|warning|incident; body is markdown.
CREATE TABLE journal_entries (
    id          INTEGER PRIMARY KEY,
    entity_type TEXT NOT NULL,
    entity_id   INTEGER NOT NULL,
    kind        TEXT NOT NULL,
    body        TEXT NOT NULL,
    created_at  TEXT NOT NULL
);
CREATE INDEX idx_journal_entity ON journal_entries (entity_type, entity_id);
