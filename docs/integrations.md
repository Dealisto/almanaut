[← Documentation index](README.md) · [almanaut](../README.md)

# Notifications & integrations

Each integration below is switched on by environment variables, described in
full in [Configuration](configuration.md).

## Expiry notifications (ntfy & Discord)

Set `ALMANAUT_NTFY_URL` to an [ntfy](https://ntfy.sh) topic URL and/or
`ALMANAUT_DISCORD_WEBHOOK_URL` to a Discord
[incoming-webhook](https://support.discord.com/hc/en-us/articles/228383668)
URL. Almanaut pushes an alert when a certificate, hardware warranty, or
subscription renewal falls within `ALMANAUT_NOTIFY_WITHIN_DAYS` (default 30).

Each item notifies **once per deadline** after successful delivery. Changing
the date re-arms the alert, even when the new date stays inside the window
(for example, a weekly subscription renewed within a 30-day alert window).
Moving it beyond the window re-arms it for when it becomes due again.
The notified deadline survives restarts. When upgrading from a version that
tracked only the item, existing alerts adopt the current inventory deadline
to avoid sending every alert again immediately. The check runs at startup
and every `ALMANAUT_NOTIFY_INTERVAL`. Leave both URLs unset to disable
notifications entirely.

Expiry windows and day labels use calendar dates in the server's timezone.
Failed deliveries appear as errors on **Scheduled tasks** without preventing
other items from being attempted; failed items remain eligible for the next pass.
Channels start independently, so a slow endpoint does not delay delivery to a
healthy one. An item is marked successful only after every configured channel
succeeds; a retry can therefore repeat an alert on a channel that already
accepted it. When a pass times out, the next pass starts with the next eligible
item rather than repeatedly blocking behind the same alert. This rotation is
kept in memory and resets on restart.

Notification URLs must point directly to their final ntfy or Discord endpoint.
HTTP redirects are treated as failed deliveries, including redirects that
preserve the POST method; this prevents a redirected request from being mistaken
for a delivered alert. HTTP response bodies are omitted from error logs
because they may contain secret-bearing URLs or reflected credentials.

## Outbound webhooks

Set `ALMANAUT_WEBHOOKS_ENABLED=true` to push a signed HTTP payload to your own
endpoints on entity create/update/delete. Admins manage endpoints on the
**Webhooks** page (`/webhooks`): add one by URL, optionally scope it to
specific entity types and events (leave everything unchecked to fire on
everything), and enable/disable or edit it later.

A signing secret is generated when the endpoint is created and shown **once**
— receivers verify each delivery's `X-Almanaut-Signature: sha256=<hex>` header
with it. Each delivery also carries an `X-Almanaut-Delivery: <id>` header,
stable across retries, for receiver-side idempotency. Failed deliveries retry
with backoff up to `ALMANAUT_WEBHOOK_MAX_ATTEMPTS` times.

Deliveries are saved in SQLite in the same transaction as the inventory change.
A rolled-back change sends nothing, and an event committed just before a restart
is resumed automatically. The signed body, delivery ID, retry count and next
attempt time are preserved. Receivers should deduplicate by delivery ID: a
crash after the receiver accepts a request but before its acknowledgment is
saved can cause a repeated delivery. Exhausted retries are logged and removed.

The queue holds up to 10,000 deliveries and 64 MiB of retained request data.
If either bound is reached, the inventory mutation rolls back and returns HTTP
`503` with `Retry-After`; events are not silently dropped to make room.
Administrators can see the pending count on **Webhooks** and check unavailable
receivers. Disabling, deleting, or changing an endpoint's URL, secret or filters
cancels its queued deliveries. A delivery already in progress may still complete.

On shutdown, active requests are canceled and unfinished deliveries remain for
the next startup. Interrupted attempts are retried without consuming additional
retry budget. Crash recovery waits for the previous worker's
lease to expire (normally the request timeout plus 30 seconds).
Setting `ALMANAUT_WEBHOOKS_ENABLED=false` stops delivery and does not queue new
events; existing rows remain until webhooks are enabled again or their endpoint
is changed. Queue data is included in complete database backups, not YAML
inventory exports. An inventory import preserves previously queued historical
events and does not generate a bulk set of replacement events.

Confirmed discovery imports and reviewed field changes also emit events;
matching an existing record without changing it does not. New or updated
discovered services trigger Uptime Kuma sync when that integration is enabled.

## Uptime Kuma sync

Set `ALMANAUT_KUMA_URL`, `ALMANAUT_KUMA_USER`, and `ALMANAUT_KUMA_PASS` (or
`_FILE`) to one-way sync services that have an http(s) URL to
[Uptime Kuma](https://github.com/louislam/uptime-kuma) HTTP monitors — the
sync is disabled unless all three are set. Check status and trigger an
on-demand resync from the **Kuma** admin page (`/kuma`).

Things to know:

- Configure the final Kuma base URL directly. HTTP redirects during the
  WebSocket handshake are rejected so login credentials stay at the configured
  endpoint.
- Kuma has no public API for monitor CRUD, so almanaut uses Kuma's internal
  socket.io API. **Pin your Kuma version** — that API can change between
  releases.
- 2FA-enabled Kuma accounts are not supported — use a dedicated account
  without 2FA.
- Only monitors that almanaut itself created are ever touched — monitors you
  made by hand in Kuma are left alone.
- A whole-inventory YAML import does not itself trigger a sync — use **Sync
  now** on `/kuma` afterwards.
- If a create's acknowledgment is lost mid-flight (e.g. Kuma restarting during
  a sync), the monitor can end up created in Kuma with no matching almanaut
  record; almanaut logs this case, and the orphaned monitor should be removed
  by hand in Kuma before the next sync creates a duplicate.
- An unexpected sync panic is recorded as a failed last sync on the admin page;
  the worker stays available for the next trigger.

---

**See also:** [Configuration](configuration.md) · [Background jobs](background-jobs.md) · [Monitoring](monitoring.md)
