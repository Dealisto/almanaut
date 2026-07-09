-- Role-based access control: each login account has a fixed built-in role.
-- Existing rows backfill to 'admin' so upgrades never silently drop privileges
-- (every pre-RBAC user was effectively a full administrator). Application code
-- sets the role explicitly on insert; the DEFAULT is a backfill + safety net.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'admin';
