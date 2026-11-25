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

## What the file contains

Every entity type, with its relationships, tags, journal entries and custom
fields (definitions and values), plus the host type catalog and its display
labels. Discovery associations preserve each source key, external resource ID
and target host/service ID. Records keep their IDs, so references and discovery
identities survive the round trip.

It does **not** contain anything specific to one instance: users, sessions,
API tokens and 2FA, webhooks, saved views, the authentication audit log, the
history (changelog), [attachments](inventory-model.md#attachments), agent
bindings and reports, Uptime Kuma monitor mappings, and discovery runs.
Discovery source configuration and credentials are not included. For a complete
backup, preserve the SQLite database and separately managed configuration and
secret files. Attachment contents are stored inside the database.

## Complete backups

For a simple complete backup, stop the Almanaut process or container cleanly,
then copy its data directory and configuration before restarting it. Protect
these copies as credentials: the database includes users, API token hashes,
TOTP secrets, and integration settings.

Do not copy only `almanaut.db` while the server is running. SQLite uses WAL mode,
so recent committed data may still be in `almanaut.db-wal`; copying the live
database and WAL separately also does not guarantee a consistent backup. Use a
SQLite online backup operation for a live full backup, or stop the server first.
YAML export is safe while running but includes only the inventory described above.

Restore a full backup with the server stopped and use the matching configuration.
Check a restored copy before relying on it, including attachments and access to
the administrator account.

## Export format compatibility

Exports use **format version 3**, adding `discovery_bindings` to the host type
catalog introduced in version 2. Version 1 and 2 files remain importable; files
without discovery associations restore with none. Keep the same configured
source keys after moving an inventory to another instance so its associations
still identify the intended sources.

Version 1 files without a catalog restore the four built-in host types with
their original labels. Version 2 and 3 catalogs must contain all four built-in
names, unique type names, and an entry for every host's type. Discovery
associations must have a valid source key, unique source/external-ID pair and
an existing host or service target. Invalid catalogs or associations reject
the entire import before any inventory is replaced.

Older Almanaut binaries reject version 3 exports rather than silently losing
identity metadata. **Keep a pre-upgrade database backup for rollback**; switching
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
Tag names are normalised the way the UI stores them (`#Prod` → `prod`). A
file written by a newer almanaut, with a higher `version`, is refused.

Uploads must contain exactly one YAML document with an explicit supported
`version`. Empty files, unknown field names (including misspelled record fields),
and additional documents are rejected before any data is replaced. Record IDs
must be positive and unique within each record type. To deliberately restore
an empty inventory, use a valid versioned export containing no records.

## Additive CSV import

The **Add or update records** section imports a CSV for a single entity type
without touching other records. Select the record type, choose the file, and
confirm the import. If validation fails, the error is shown next to the form;
select the corrected file again before retrying.

- For hosts, `type` is the permanent name from **Host types**, not the display
  label. Create custom types before importing a CSV that refers to them.
- The header row uses the entity's field names in `snake_case`, matching the
  YAML export (e.g. for hosts: `name,type,os,cpu,ram,disk,status,ips,notes`).
- An optional `id` column controls create vs. update: a row with an existing
  `id` **updates** that row; a blank or absent `id` **creates** a new row.
- Multi-value fields use commas inside the cell — e.g. a host's `ips` column:
  `"10.0.0.1,10.0.0.2"` (quote the cell so the commas are not column separators).
- Boolean fields (`auto_renew`, `mgmt_only`) accept `true`/`false` (also
  `1`/`0`, `yes`/`no`, `on`/`off`, `y`/`n`), ignoring case and surrounding
  whitespace. Empty values mean false; other values abort the import.
- Updating a row is a **full replace** of that row's columns present in the
  file — an omitted column is written as empty, same as clearing it in the
  edit form.
- The import is **all-or-nothing**: if any row is invalid, the page lists
  every bad row and writes nothing. Each created/updated row is recorded in
  the entity's history.

Example (`hosts.csv`):

```csv
name,type,ips
edge-router,physical,"10.0.0.1,10.0.0.254"
web-01,vm,10.0.0.10
```

---

**See also:** [The inventory model](inventory-model.md) · [JSON API](api.md)
