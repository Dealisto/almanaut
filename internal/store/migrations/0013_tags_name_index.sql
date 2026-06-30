-- Index for tag lookups keyed on name. The existing UNIQUE(entity_type,
-- entity_id, name) index cannot serve these because name is not its left-most
-- column, so TagRepo.Counts (GROUP BY name) and TagRepo.ListByName
-- (WHERE name = ?) full-scanned the tags table. (TagRepo.Search uses
-- instr(name, ?), a substring scan that no B-tree index can accelerate, so it
-- is unaffected.)
CREATE INDEX idx_tags_name ON tags (name);
