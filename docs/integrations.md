[← Documentation index](README.md) · [almanaut](../README.md)

# Notifications & integrations

## Expiry notifications (ntfy & Discord)

Set `ALMANAUT_NTFY_URL` to an [ntfy](https://ntfy.sh) topic URL and/or
`ALMANAUT_DISCORD_WEBHOOK_URL` to a Discord
[incoming-webhook](https://support.discord.com/hc/en-us/articles/228383668)
URL. Almanaut pushes an alert when a certificate, hardware warranty, or
subscription renewal falls within `ALMANAUT_NOTIFY_WITHIN_DAYS` (default 30).

Each item notifies **once** per configured channel; renewing it (pushing the
date beyond the window) re-arms it for next time. The check runs at startup
and every `ALMANAUT_NOTIFY_INTERVAL`. Leave both URLs unset to disable
notifications entirely.

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

## Uptime Kuma sync

Set `ALMANAUT_KUMA_URL`, `ALMANAUT_KUMA_USER`, and `ALMANAUT_KUMA_PASS` (or
`_FILE`) to one-way sync services that have an http(s) URL to
[Uptime Kuma](https://github.com/louislam/uptime-kuma) HTTP monitors — the
sync is disabled unless all three are set. Check status and trigger an
on-demand resync from the **Kuma** admin page (`/kuma`).

Things to know:

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
