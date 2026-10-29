[← Documentation index](README.md) · [almanaut](../README.md)

# Export & import

The whole inventory round-trips through a single YAML file. **Data → Export**
(or `GET /export`) downloads `almanaut-export.yaml`; **Data → Import** uploads
one back. Use it to move an inventory between instances, or as a portable copy
of your data.

> ⚠️ Import **replaces the entire inventory** — every existing record is
> deleted and re-created from the file. It is not a merge. The import form
> makes you tick a confirmation checkbox first.

## What the file contains

Every entity type, with its relationships, tags, journal entries and custom
fields (definitions and values). Records keep their ids, so references between
them survive the round trip.

It does **not** contain anything specific to one instance: users, sessions,
API tokens and 2FA, webhooks, saved views, the authentication audit log, the
history (changelog), [attachments](inventory-model.md#attachments), agent
bindings and reports, Uptime Kuma monitor mappings, and discovery runs. For a
complete backup, copy the SQLite database file instead.

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

## Additive CSV import

The **Data** page also imports a CSV for a single entity type without touching
any other data — the complement to the whole-inventory YAML import.

- The header row uses the entity's field names in `snake_case`, matching the
  YAML export (e.g. for hosts: `name,type,os,cpu,ram,disk,status,ips,notes`).
- An optional `id` column controls create vs. update: a row with an existing
  `id` **updates** that row; a blank or absent `id` **creates** a new row.
- Multi-value fields use commas inside the cell — e.g. a host's `ips` column:
  `"10.0.0.1,10.0.0.2"` (quote the cell so the commas are not column separators).
- Boolean fields (`auto_renew`) accept `true`/`false` (also `1`/`0`, `yes`/`no`).
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
