ALTER TABLE backups ADD COLUMN expected_interval_hours INTEGER NOT NULL DEFAULT 0 CHECK (expected_interval_hours BETWEEN 0 AND 87600);
CREATE TABLE backup_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    backup_id INTEGER NOT NULL REFERENCES backups(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('success', 'failed')),
    started_at TEXT NOT NULL DEFAULT '',
    completed_at TEXT NOT NULL,
    duration_seconds INTEGER,
    size_bytes INTEGER,
    message TEXT NOT NULL DEFAULT '',
    received_at TEXT NOT NULL,
    UNIQUE (backup_id, run_id)
);
CREATE INDEX idx_backup_runs_latest ON backup_runs(backup_id, completed_at DESC, id DESC);
