-- Application settings survive inventory replacement and are not inventory exports.
CREATE TABLE health_rule_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    settings TEXT NOT NULL
);
