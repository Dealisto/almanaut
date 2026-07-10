-- Authentication audit log (#100): one row per auth-relevant event (login
-- success/failure, logout, API-token use, session revocation, later 2FA/SSO).
-- Not a user-editable entity; pruned by a configurable retention window.
CREATE TABLE auth_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT    NOT NULL,             -- 'login_success' | 'login_failure' | 'logout' | 'token_used' | 'session_revoked' | ...
    username   TEXT    NOT NULL DEFAULT '',  -- attempted or authenticated username
    user_id    INTEGER NOT NULL DEFAULT 0,   -- resolved user id, 0 when unknown (e.g. failed login)
    source_ip  TEXT    NOT NULL DEFAULT '',  -- direct peer address
    detail     TEXT    NOT NULL DEFAULT '',  -- optional free-text (e.g. token label)
    created_at TEXT    NOT NULL              -- RFC3339
);
CREATE INDEX idx_auth_events_created ON auth_events (created_at);
CREATE INDEX idx_auth_events_user ON auth_events (username, created_at);
