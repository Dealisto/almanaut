[← Documentation index](README.md) · [almanaut](../README.md)

# Export & import

The whole inventory round-trips through a single YAML file. On **Import &
export** (`/data`), **Download YAML** (or `GET /export`) downloads
`almanaut-export.yaml`. Open **Restore from a YAML file** in the **Replace the
inventory** section to upload one back. Use it to move an inventory between
instances, or as a portable copy of your data.

> ⚠️ Import **replaces the entire inventory** — every existing record is
> deleted and re-created from the file. It is not a merge. The import form
> makes you tick a confirmation checkbox first.

Import confirmation must be submitted with the upload. A query-string `confirm`
cannot authorize an import. CSV imports likewise take their entity type from
the uploaded form, so a URL parameter cannot replace the selected type.

## What the file contains

Every entity type, with its relationships, tags, journal entries and custom
fields (definitions and values), plus the host type catalog and its display
labels. Discovery associations preserve each source key, external resource ID
and target host/service ID. Records keep their IDs, so references and discovery
identities survive the round trip.

It does **not** contain anything specific to one instance: users, sessions,
API tokens and 2FA, webhook endpoints and pending deliveries, saved views, the
authentication audit log, the history (changelog),
[attachments](inventory-model.md#attachments), agent
bindings and reports, Uptime Kuma monitor mappings, and discovery runs.
Discovery source configuration and credentials are not included. For a complete
backup, preserve the SQLite database and separately managed configuration and
secret files. Attachment contents are stored inside the database.

## Complete backups

A **database backup** contains all SQLite data: inventory, attachments, users,
password hashes, TOTP secrets, API tokens, sessions, history, integration settings,
and pending outbound deliveries. It does **not** contain environment settings,
discovery source configuration, mounted secret files, or deployment/proxy files.
Keep those separately to recover a complete deployment. Protect backup files as
credentials.

### Create and verify a database backup

Use explicit filesystem paths; the destination directory must already exist:

```bash
almanaut backup --source /data/almanaut.db --destination /backups/manual-backup.db
almanaut backup-verify --source /backups/manual-backup.db
```

`backup` takes a consistent SQLite snapshot using `VACUUM INTO`, including
committed WAL data while the server is running. It creates a new private file,
verifies the snapshot, and removes the new file if the operation fails. Existing
destinations are never overwritten. Both commands require an existing source
database and accept `--timeout` (default `10m`). Exit codes are `0` for success,
`1` for an operation failure, and `2` for invalid arguments.

The commands run before server configuration is loaded: they do not create a
missing source, migrate it, reset an administrator, or start background jobs and
integrations. Verification opens the database read-only and checks SQLite
integrity, foreign keys, and the original Almanaut schema/migration markers.
Older schemas are accepted; verification does not assert compatibility with the
current server binary, or replace a restoration drill that tests login and files.

Do not copy only `almanaut.db` while the server is running. SQLite uses WAL mode,
so recent committed data may still be in `almanaut.db-wal`; copying the live
database and WAL separately also does not guarantee a consistent backup. The
backup command handles this. Alternatively, stop the server cleanly before
copying its data directory. YAML export is safe while running but includes only
the inventory described above.

### Scheduled database backups

Set `ALMANAUT_BACKUP_DIR` to enable the database-backup task. Configuration,
interval, timeout, and retained snapshot count are documented in
[Configuration](configuration.md). The existing scheduled-tasks page provides
manual triggering and the current run status. The admin-only **Database backups**
page (`/database-backups`) reads successful snapshots from disk, so their history
remains available after a restart.

Each successful run creates a private `almanaut-backup-<id>` directory containing
`database.sqlite` and `manifest.json`. The manifest records the source path,
creation time, server version, schema version, size, and SHA-256 checksum. It is
written only after snapshot verification succeeds. The manifest is local backup
metadata, not an export of external configuration or secrets.

Retention runs only after a successful backup and only recognizes complete
snapshot directories belonging to the same source path. Unrelated files,
symlinks, incomplete directories, and directories containing extra files are
left alone. A failed snapshot does not trigger retention; a retention error is
reported after the new verified snapshot has been saved. A process crash can
leave an incomplete directory without a valid manifest; inspect and remove it manually.
The configured count applies to recognized snapshots, not every file in the
backup directory. Store backups on a separate volume or copy them off-machine
when protection against loss of the data disk is required.

### Restore a database backup

1. Verify the selected backup and recover its matching configuration and secrets.
   Prefer the server version recorded in its manifest or otherwise recorded when
   taking the backup.
2. Stop Almanaut completely. Preserve its existing data directory for rollback,
   including any WAL/SHM files, and prepare a new private data directory.
3. Copy the verified snapshot into the new directory as `almanaut.db`, preserving
   private permissions and the service account's access. Do not mix a restored
   database with WAL/SHM files from the previous database. Keep the original
   backup unchanged.
4. Test the restored copy in isolation, with outbound integrations and discovery
   disabled. Confirm administrator login and download an attachment. A database
   integrity check alone cannot prove these application-level behaviors.
5. Start the intended instance with its matching configuration after the check.

Full restoration also restores old sessions, token state, and queued outbound
events. When integrations are enabled, pending deliveries may be retried. Account
for these restored states before reconnecting an isolated restore test or
returning the restored instance to service. There is no live database replacement
or browser restore operation in this workflow.

## Export format compatibility

Exports use **format version 4**, adding each host's `agent_protected_fields`,
structured `network_interfaces` and `ip_addresses`, and each backup's
`expected_interval_hours` monitoring policy.
Versions 1, 2 and 3 remain importable; files without a protection policy restore
with no protected fields. Version 3 introduced `discovery_bindings`, and version
2 introduced the host type catalog. Files without discovery associations restore
with none. Keep the same configured source keys after moving an inventory to
another instance so its associations still identify the intended sources.

Version 1 files without a catalog restore the four built-in host types with
their original labels. Version 2 and later catalogs must contain all four built-in
names, unique type names, and an entry for every host's type. Discovery
associations must have a valid source key, unique source/external-ID pair and
an existing host or service target. Invalid catalogs or associations reject
the entire import before any inventory is replaced.

Older Almanaut binaries reject version 4 exports rather than silently losing
agent protection. **Keep a pre-upgrade database backup for rollback**; switching
the binary back does not undo database migrations, and the new export cannot
be restored by an older importer. Restore that backup together with the older
binary when rolling back.

## What import does to the rest

Data outside the file is reconciled with the imported inventory:

- **Attachments** follow their entity's id: an entity present in the file
  keeps its attachments, and attachments of entities the file does not contain
  are deleted. Restoring this instance's own export therefore keeps them.
  Importing a file from *another* instance, where the same id may be a
  different entity, can leave an attachment on the wrong record.
- **History** is kept, including the record of deleted entities; the import
  itself is logged as one event.
- **Derived state** — liveness status, certificate probe results, and which
  expiry notifications were already sent — is cleared and rebuilt by the
  [background jobs](background-jobs.md) on their next pass. An item already
  notified as expiring may be notified once more.
- **Discovery associations** are replaced with those in the snapshot, in the
  same transaction as their host/service targets. Importing an older file with
  no associations clears the current ones; a later confirmed discovery import
  can associate matching manual records again.
- **Agent bindings** are removed with the hosts they belonged to; an agent
  re-binds on its next report.
- **Uptime Kuma monitors** are reconciled by the next sync, which removes the
  monitor of a service the file no longer contains.

A relationship, tag, journal entry or custom-field value that points at an
entity (or field definition) missing from the file is skipped rather than
failing the import; the import's history event records how many were skipped.
The same goes for a port's connection to, or NIC attribution to, something the
file does not contain: it is cleared and counted. A connection written on one
port only is completed on the other. Two ports connected to the same port, or
a port on another host's NIC, fail the import.
Tag names are normalised the way the UI stores them (`#Prod` → `prod`).
Exports escape a stored leading `#` with one additional `#`, so a literal
`#prod` is written as `##prod` and keeps its identity on repeated restores.
This does not change the format version.
A file written by a newer almanaut, with a higher `version`, is refused.
Duplicate normalized tags on one entity, duplicate custom-field names for one
entity type, and multiple values for the same entity and field are rejected
with an error identifying the conflicting records.

Uploads must contain exactly one YAML document with an explicit supported
`version`. Empty files, unknown field names (including misspelled record fields),
and additional documents are rejected before any data is replaced. Record IDs
must be positive and unique within each record type. To deliberately restore
an empty inventory, use a valid versioned export containing no records.

Invalid input receives a detailed form error. A storage failure returns a
generic server error and logs its details on the server. In either case, the
existing inventory is preserved; replacement and its history event commit
together.

YAML and CSV imports also roll back pending writes when request cancellation
is observed before commit starts. Cancellation does not undo a completed
commit, and interruption is not immediate during every parsing or database
operation. Attachment uploads follow the same transaction rule.

## Preview and apply CSV changes

The **Add or update records** section accepts one entity type per file. Select
**Patch** (the default) or **Replace**, upload the CSV, and review the proposed
field changes before confirming. Previews include indirect changes such as
reciprocal port links. Preparing a preview saves no inventory, history, or
webhook deliveries. Invalid files show row diagnostics and cannot be applied.

- Headers use the entity's exported `snake_case` field names. Unknown or duplicate
  columns are rejected. UTF-8 BOMs and quoted multiline cells are supported.
- An existing positive `id` updates that record; an empty or absent `id` creates
  one. Repeated existing IDs in one file are rejected. Other records are kept.
- **Patch** preserves omitted columns on updates. A present empty cell explicitly
  clears the field or applies its normal form default. Required fields cannot be
  cleared. New records still need all required fields.
- **Replace** parses omitted fields as empty inputs, applying normal defaults and
  validation. Include every field you want to retain.
- Host `agent_protected_fields` is a safety exception in both modes: omitting
  the column preserves the existing policy. A quoted list such as `"ips,os"`
  replaces it; a present empty cell clears it. Valid names are `ips`, `os`, `cpu`,
  `ram`, and `disk`.
- Backup `expected_interval_hours` is also preserved when omitted in either
  mode; a present empty cell disables monitoring. See [external backup job
  reporting](backup-jobs.md).
- For hosts, `type` is the permanent name from **Host types**, not its display
  label. Create custom types before importing records that use them.
- Lists use comma-separated values inside quoted cells, such as
  `"10.0.0.1,10.0.0.2"` for host `ips`.
- Boolean fields accept `true`/`false`, `1`/`0`, `yes`/`no`, `on`/`off`, or `y`/`n`,
  ignoring case and surrounding whitespace. A present empty value means false.
- A preview accepts at most **500 rows and 8 MiB**. It expires after **15 minutes**,
  belongs to the user who uploaded it, and can be confirmed only once. Restarting
  Almanaut invalidates pending previews. At most four previews per user and 32
  globally are retained, within a 32 MiB serialized-data budget.

Apply revalidates the file and its target records inside one write transaction.
If a reviewed record changed, or changes to related records would differ from
those reviewed, the whole import is rejected: upload again to review the current
state. Generated IDs for new records are assigned on apply. All accepted rows,
change history, and durable webhook deliveries commit together; failures roll
back the entire batch. No-op updates create no history or webhook event. A failed
confirmation consumes its token, so upload again before retrying.

For existing integrations, `POST /import-csv` retains its immediate,
full-replacement behavior and requires the existing confirmation form field.
The UI uses `POST /import-csv/preview` (`type`, `mode`, `file`), followed by
`POST /import-csv/apply` (`preview_token`, `confirm`). These are writer-only form
routes with the normal CSRF checks, not bearer-token API endpoints.

Example patch (`hosts.csv`):

```csv
id,notes
42,Updated maintenance instructions
43,
```

This updates host 42's notes, clears host 43's notes, and preserves their other
fields. Omit `id` and include required fields such as `name,type` to create hosts.

---

**See also:** [The inventory model](inventory-model.md) · [JSON API](api.md)
