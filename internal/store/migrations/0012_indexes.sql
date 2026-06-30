-- Indexes for the hot lookup paths that previously full-scanned:
-- relationship lookups by either endpoint (entity detail pages, relationship
-- views, and store.Impact's per-node BFS), and tag lookups by entity.
CREATE INDEX idx_relationships_from ON relationships (from_type, from_id);
CREATE INDEX idx_relationships_to ON relationships (to_type, to_id);
CREATE INDEX idx_tags_entity ON tags (entity_type, entity_id);
