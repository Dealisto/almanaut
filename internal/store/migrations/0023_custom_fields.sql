-- Custom fields: user-defined fields attachable to any entity type.
-- Definitions describe a field (one per entity type); values hold per-entity data.
CREATE TABLE custom_field_definitions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL,
    name        TEXT NOT NULL,
    label       TEXT NOT NULL,
    kind        TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    UNIQUE(entity_type, name)
);

CREATE TABLE custom_field_values (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL,
    entity_id   INTEGER NOT NULL,
    def_id      INTEGER NOT NULL,
    value       TEXT NOT NULL,
    UNIQUE(entity_type, entity_id, def_id)
);

CREATE INDEX idx_cfv_entity ON custom_field_values(entity_type, entity_id);
