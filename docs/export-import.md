[← Documentation index](README.md) · [almanaut](../README.md)

# Export & import

The whole inventory round-trips through a single YAML file. **Data → Export**
(or `GET /export`) downloads `almanaut-export.yaml`; **Data → Import** uploads
one back. This is your backup/restore and migration path (attachments
excepted — they live only in the database file).

> ⚠️ Import **replaces the entire inventory** — every existing record is
> deleted and re-created from the file. It is not a merge. The import form
> makes you tick a confirmation checkbox first.

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
