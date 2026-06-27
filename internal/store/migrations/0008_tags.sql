CREATE TABLE tags (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL,
    entity_id   INTEGER NOT NULL,
    name        TEXT NOT NULL,
    UNIQUE (entity_type, entity_id, name)
);
