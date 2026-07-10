-- Saved views (#97): a named filter/sort query string for a list page, private
-- to the user who saved it. A view is just a stored URL query string, replayed
-- against the entity's list route. user_id 0 is the shared owner when auth is
-- disabled (single-user mode).
CREATE TABLE saved_views (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL,
    entity_type TEXT    NOT NULL,             -- singular type key, e.g. "host"
    name        TEXT    NOT NULL,
    query       TEXT    NOT NULL DEFAULT '',  -- URL query string, e.g. "sort=Name&dir=asc"
    created_at  TEXT    NOT NULL              -- RFC3339
);
CREATE INDEX idx_saved_views_user ON saved_views (user_id, entity_type, name);
