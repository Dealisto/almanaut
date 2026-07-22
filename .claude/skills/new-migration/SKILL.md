---
name: new-migration
description: Scaffold the next numbered SQLite migration under internal/store/migrations/ following almanaut's append-only convention.
disable-model-invocation: true
---

# new-migration

Create the next schema migration for almanaut. Migrations live in
`internal/store/migrations/` and are applied in filename order by
`internal/store/migrate.go` (auto-discovered via an `embed` glob + sort, so
**just creating the file is enough** — no registration needed).

## Rules

- **Append-only.** Never edit an existing migration; a `PreToolUse` hook blocks
  it. Schema changes always go in a new, higher-numbered file.
- **Naming:** `NNNN_<snake_case_description>.sql`, zero-padded to 4 digits.
- **Forward-only safety.** The file must apply cleanly on a DB that already ran
  every prior migration:
  - new `NOT NULL` column → give it a `DEFAULT`, or add nullable then backfill.
  - `UNIQUE` constraints must not be violated by existing rows.
  - SQLite `ALTER TABLE` is limited (no drop/alter column in old syntax) — use a
    table-rebuild (`CREATE new` → `INSERT … SELECT` → `DROP old` → `ALTER … RENAME`)
    when needed.
- The whole file runs inside one transaction. The DB has `foreign_keys(1)` on.

## Steps

1. Find the highest existing number:
   ```bash
   ls internal/store/migrations/ | sort | tail -1
   ```
2. Create `internal/store/migrations/<next>_<name>.sql` with the DDL. Match the
   style of existing files (lowercase keywords aligned, `id INTEGER PRIMARY KEY
   AUTOINCREMENT`, explicit `UNIQUE (...)` where appropriate).
3. Update the matching `domain` struct and `store` repo so the Go code agrees
   with the new schema (column names, types, nullability).
4. Optionally invoke the `migration-reviewer` subagent to validate before commit.
   Do **not** run `go test` locally (App Control blocks it; CI is the gate).

## Reference: existing migration

```sql
CREATE TABLE tags (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL,
    entity_id   INTEGER NOT NULL,
    name        TEXT NOT NULL,
    UNIQUE (entity_type, entity_id, name)
);
```
