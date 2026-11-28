# External backup job reporting

Backup inventory records describe how a host or service is backed up. External
job reports add evidence that the corresponding backup tool completed a run.
This feature does not execute backup software or verify that its payload can be
restored. Almanaut's own database snapshots are a separate feature documented in
[Import, export, and database backups](export-import.md).

## Monitor a job

Create or edit a backup record and set **Expected interval (hours)** to the maximum
acceptable age of a successful completed run. The allowed range is 0–87600 hours;
zero disables monitoring. Choose an interval that includes the job's normal run
time and expected scheduling margin. The descriptive `frequency` and manually
entered `last_run` date do not establish successful execution.

Follow **Reported runs and job health** from the record's details, or open
`/backups/<id>/runs`. The page shows the most recent 200 completed runs, ordered by
completion time. Job health is:

| State | Meaning |
|---|---|
| Unmonitored | Expected interval is zero. |
| Never | Monitoring is enabled but no completed report exists. |
| Failed | The newest completed report has status `failed`. |
| Stale | The newest successful report is older than the expected interval. |
| Healthy | The newest report succeeded within the expected interval. |

The health report includes never-reported, failed, and stale jobs under **Backup
job health**. Administrators can enable or disable that rule in health settings.
A late-arriving report does not supersede a newer completion. Ties use the order
reports were accepted.

## Report completed runs

Use a read-write API token belonging to an editor or administrator. Read-only,
viewer, and agent-scoped credentials cannot submit reports. As with other API
writes, browser session cookies alone cannot authorize this endpoint when
authentication is enabled.

```http
POST /api/backups/42/runs
Authorization: Bearer <read-write-token>
Content-Type: application/json

{
  "run_id": "restic-home-2026-11-27T03:00:00Z",
  "status": "success",
  "started_at": "2026-11-27T03:00:00Z",
  "completed_at": "2026-11-27T03:04:12Z",
  "duration_seconds": 252,
  "size_bytes": 2147483648,
  "message": "Completed scheduled repository backup"
}
```

Required fields are `run_id`, `status`, and `completed_at`:

- `run_id` is an external identifier unique within this backup's retained history,
  at most 200 bytes. Reuse it when retrying the same report.
- `status` is `success` or `failed`; only completed jobs are accepted.
- `completed_at` is an RFC3339 timestamp, at most five minutes in the future.
- Optional `started_at` must not follow completion.
- Optional `duration_seconds` is an integer between 0 and 315576000; `size_bytes`
  is a nonnegative integer. Missing or null values remain unknown, distinct from
  zero. Duration is reported metadata, not independently measured by Almanaut.
- Optional `message` accepts up to 4096 bytes. Keep backup contents and credentials
  out of reports. JSON requests are limited to 16 KiB; unknown fields are rejected.

New reports return **201** and the stored report. An identical retry returns
**200** with the same stored report and creates no additional history or webhook.
A reused ID with different data returns **409**. Timestamps with equivalent UTC
instants normalize to the same stored representation.

Almanaut retains 200 reports per backup. Once that window is full, an unknown run
ID whose completion time is at or before its oldest retained report is rejected
with **409**. This includes retries whose original report was pruned. Retention
never lets a late report remove the newest evidence. The most recent reports are
also available at `GET /api/backups/<id>/runs` using normal API read credentials.

Each accepted report, its history entry, and any durable webhook delivery are
committed together. Failed writes or a full webhook queue leave all three
unchanged. A successful report emits the existing `backup` / `updated` webhook
with the backup fields and a `reported_run` object in its data. The inventory's
manually documented `last_run` date is unchanged.

## Compatibility and restore

`expected_interval_hours` is included in YAML/JSON inventory exports. Omitting it
or sending null on an existing backup update preserves its policy; explicit zero
disables it. In CSV, omitting the column preserves the policy in both update modes,
and a present empty cell disables monitoring. Historical inventory snapshots
without the field restore with monitoring disabled.

Run history is operational state and is excluded from YAML inventory exports.
Replacing the inventory clears reports belonging to the replaced backup records.
A full SQLite database backup includes job reports, history, and webhook outbox
state; restore procedures must account for pending outbound deliveries.
