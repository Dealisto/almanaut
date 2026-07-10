<div align="center">

# Almanaut

**A lightweight, self-hosted homelab inventory & documentation tool.**
*"NetBox for the rest of us."*

[![CI](https://github.com/Dealisto/almanaut/actions/workflows/ci.yml/badge.svg)](https://github.com/Dealisto/almanaut/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Container image](https://img.shields.io/badge/ghcr.io-dealisto%2Falmanaut-2496ed?logo=docker&logoColor=white)](https://github.com/Dealisto/almanaut/pkgs/container/almanaut)

One Go binary · SQLite storage · server-rendered UI · zero client-side JS frameworks

![almanaut dashboard](docs/screenshots/dashboard.png)

</div>

Almanaut keeps track of everything in your homelab — machines, services,
networks, certificates, backups, subscriptions, and how they all relate — in
one place, with the operational extras that usually require three more tools:
IPAM, expiry alerts, auto-discovery, a JSON API, and Prometheus metrics.

It is deliberately small: a single binary, a single SQLite file, and a
server-rendered UI. Back it up by copying one file (or one YAML export).

> **Status:** early development (v0.1). Expect rough edges; the export/import
> path makes upgrades and migrations safe.

## Features

**Inventory**

- **15 entity types** — hosts, services, networks, domains, certificates,
  backups, hardware, subscriptions, accounts, sites, locations, racks,
  contacts, VLANs, and IP reservations
- **Relationships & a neighbourhood graph** on every detail page (a service
  *runs on* a host, *is backed up by* a backup, *administered by* a contact…)
- **Global search** across every entity type, tags, and custom-field values
- **Dashboard** with a "needs attention" panel, inventory summary, and recent
  activity
- **Custom fields** — add your own typed fields (text, number, bool, date) to
  any entity type
- **Attachments** — upload files (manuals, invoices, configs) to any entity
- **Tags** on everything, **journal** notes, and an automatic field-level
  **change history** per entity plus a global `/history` feed

**Networking & physical layout**

- **IPAM** — per-network usage, capacity, next-free IP, VLANs, and named IP
  reservations (DHCP pools, reserved blocks)
- **Sites → locations → racks** hierarchy, with a rendered **U elevation** for
  each rack showing its occupants at their positions

**Automation & integrations**

- **Auto-discovery** from Docker (via the socket), a network subnet scan, and
  Proxmox VE — discovery only ever *creates* records, never overwrites yours
- **Expiry notifications** via [ntfy](https://ntfy.sh) and/or Discord for
  certificates, warranties, and renewals
- **Outbound webhooks** — signed HTTP payloads on entity create/update/delete
- **Uptime Kuma sync** — one-way sync of your services to Kuma HTTP monitors
- **Read-write JSON API** with per-user, scoped API tokens, a built-in
  reference page, and a generated OpenAPI 3 spec
- **Prometheus `/metrics`** endpoint

**Operations**

- **Mandatory login** with **role-based access control** (admin / editor /
  viewer), per-username login throttling, and session-cookie auth
- **YAML export/import** of the whole inventory, plus additive per-entity
  **CSV import**
- **Dark mode** (System / Light / Dark), Docker `HEALTHCHECK`, multi-arch
  images (`amd64` / `arm64`), distroless non-root container

## Screenshots

**Relationship graph** — each entity's neighbourhood is drawn on its detail
page (here, a host with the VM it runs on and the services running on it):

![relationship neighbourhood graph](docs/screenshots/graph.png)

**Dark mode** — a built-in System / Light / Dark switch; System follows your OS:

![almanaut in dark mode](docs/screenshots/dashboard-dark.png)

## Quick start

### Docker

```bash
docker run --rm -p 8080:8080 -v almanaut-data:/data ghcr.io/dealisto/almanaut:dev
```

Open http://localhost:8080 and log in with the admin credentials printed in
the container log (see [First login](#first-login)).

Images are published to GHCR automatically: `:dev` tracks `master`, and a
tagged release (`vX.Y.Z`) publishes `:X.Y.Z`, `:X.Y`, and `:latest`. Images
are multi-arch (`linux/amd64` and `linux/arm64`).

The container runs as a non-root user (uid `65532`). A fresh named volume (as
above) inherits the right ownership automatically. If you instead bind-mount a
host directory (`-v /host/path:/data`), make it writable by that uid first —
`sudo chown 65532:65532 /host/path` — otherwise the database cannot be created.

### Docker Compose

Drop this into `docker-compose.yml` and run `docker compose up -d`:

```yaml
services:
  almanaut:
    image: ghcr.io/dealisto/almanaut:dev
    container_name: almanaut
    ports:
      - "8080:8080"
    volumes:
      - almanaut-data:/data
      # Uncomment to enable Docker container auto-discovery (read-only):
      # - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      # All optional — see the Configuration table below. A few common ones:
      # ALMANAUT_AUTH_USER: admin        # seeds the initial admin account
      # ALMANAUT_AUTH_PASS: change-me    # seeds the initial admin account
      # ALMANAUT_ENABLE_NETWORK_SCAN: "true"
      # ALMANAUT_NTFY_URL: https://ntfy.sh/my-homelab
      TZ: Etc/UTC
    restart: unless-stopped

volumes:
  almanaut-data:
```

The image ships its own `HEALTHCHECK`, so `docker compose ps` reports the
container's health directly.

### Prebuilt binaries

Each tagged release has a [GitHub Release](https://github.com/Dealisto/almanaut/releases)
with prebuilt, statically linked binaries for Linux, macOS, and Windows
(`amd64` and `arm64`), plus a `checksums.txt`. Download the archive for your
platform, verify it, and run it:

```bash
sha256sum -c checksums.txt --ignore-missing
tar xzf almanaut_1.0.0_linux_amd64.tar.gz
ALMANAUT_DATA_DIR=./data ./almanaut
```

Release channels at a glance:

| Channel | What you get |
|---|---|
| Container `:X.Y.Z` / `:X.Y` / `:latest` | Multi-arch images on GHCR for a tagged release |
| Container `:dev` | Rolling image built from `master` |
| GitHub Release binaries | Versioned archives + checksums for a tagged release |

See [CHANGELOG.md](CHANGELOG.md) for what changed in each release.

### From source

Requires Go 1.26+.

```bash
go build -o almanaut .
ALMANAUT_DATA_DIR=./data ./almanaut
```

### First login

On first startup, if the user table is empty, almanaut creates one admin
account:

- If `ALMANAUT_AUTH_USER` / `ALMANAUT_AUTH_PASS` are set, it seeds the admin
  with that username/password (username defaults to `admin` if only the
  password is set).
- Otherwise it creates username `admin` with a **random password printed once
  to the server log**, as a banner that looks like this:

  ```
  ========================================================
  Almanaut created an initial admin account.
    username: admin
    password: 7f3kQ9z...
  Log in and change it. This is shown only once.
  ========================================================
  ```

  Copy that password immediately — it is not stored anywhere in recoverable
  form and is never logged again.

### Try it with sample data

Want to see the app populated before entering your own data? This repo ships a
small example homelab (three hosts, some services, a network, a certificate, a
backup, and the relationships between them). Grab
[`examples/inventory.yaml`](examples/inventory.yaml), then go to **Data →
Import**, upload it, tick the confirmation box, and import. You'll land on a
populated dashboard with a browsable relationship graph.

Since import wipes existing data, only load the sample into a fresh instance
(or export your real data first).

## Authentication & access control

Almanaut **requires a login** — every page and the whole JSON API sit behind a
session-cookie auth check, with no way to run it open.

### Roles

Every user has one of three built-in roles (there are no custom roles):

| Role | Can do |
|---|---|
| **admin** | Everything, including managing users, webhooks, custom fields, and integrations |
| **editor** | Create, edit, and delete inventory |
| **viewer** | Read-only access to everything |

Admins manage accounts at **Users** (`/users`): create users (viewer by
default), change roles, reset passwords, and delete accounts. The last
remaining user cannot be deleted, so you can't lock yourself out entirely.
Each user changes their own password at `/account/password`.

### Sessions & login throttling

Sessions are server-side (stored in SQLite), cookie-based, and last 30 days;
use the **Logout** button to end one early. Failed logins are throttled
per-username: after 5 consecutive failures, further attempts for that username
are refused for 15 minutes (state is in-memory and resets on restart).

`/api/*` returns a plain `401` JSON error when called without valid
credentials (a session cookie or a bearer token). `/healthz` and `/version`
are the only endpoints that bypass the login, so container health probes keep
working.

### Lockout recovery

Locked out? Set `ALMANAUT_RESET_ADMIN=true` and restart — almanaut resets the
admin's password (using `ALMANAUT_AUTH_PASS` if set, otherwise a fresh random
one) and logs the new value the same way as the first-run banner. **Unset
`ALMANAUT_RESET_ADMIN` afterwards**, or every restart will reset the password
again.

Note that `ALMANAUT_AUTH_USER` / `ALMANAUT_AUTH_PASS` only seed the *initial*
admin (or feed `ALMANAUT_RESET_ADMIN`); they are **not** HTTP Basic auth
credentials and are not checked on every request. The `_FILE` convention
applies to `ALMANAUT_AUTH_PASS` (see [Secrets from files](#secrets-from-files)).

## Configuration

All configuration is via environment variables; everything is optional.

| Variable                      | Default              | Description                                    |
|-------------------------------|----------------------|------------------------------------------------|
| `ALMANAUT_ADDR`               | `:8080`              | TCP listen address                             |
| `ALMANAUT_DATA_DIR`           | `./data`             | Directory for the SQLite database              |
| `ALMANAUT_DOCKER_SOCKET`      | `/var/run/docker.sock` | Path to the Docker socket for auto-discovery |
| `ALMANAUT_ENABLE_NETWORK_SCAN` | `false`              | Enable the opt-in subnet scan                  |
| `ALMANAUT_SCAN_SUBNET`        | (empty)              | Default subnet (CIDR) pre-filled in the scan form |
| `ALMANAUT_PROXMOX_URL`        | (empty)              | Proxmox VE API base URL (e.g. `https://pve.lan:8006`); enables Proxmox discovery when set with a token |
| `ALMANAUT_PROXMOX_TOKEN`      | (empty)              | Proxmox API token (`user@realm!tokenid=secret`) |
| `ALMANAUT_PROXMOX_INSECURE`   | `false`              | Skip TLS verification for a self-signed Proxmox certificate |
| `ALMANAUT_AUTH_USER`          | `admin`              | Seeds the username of the initial admin account created on first startup |
| `ALMANAUT_AUTH_PASS`          | (empty)              | Seeds the password of the initial admin account; a random password is generated and logged once when unset |
| `ALMANAUT_RESET_ADMIN`        | `false`              | Reset the admin password at startup (lockout recovery) and log the new value; unset it again afterwards |
| `ALMANAUT_SECURE_COOKIES`     | `false`              | Force the `Secure` flag on cookies; set to `true` when serving HTTPS through a TLS-terminating reverse proxy |
| `ALMANAUT_NTFY_URL`           | (empty)              | ntfy topic URL for expiry alerts (e.g. `https://ntfy.sh/my-homelab`); empty disables notifications |
| `ALMANAUT_NTFY_TOKEN`         | (empty)              | Optional bearer token for a protected ntfy topic (supports the `_FILE` convention) |
| `ALMANAUT_DISCORD_WEBHOOK_URL` | (empty)             | Discord incoming-webhook URL for expiry alerts; empty disables the channel (supports the `_FILE` convention) |
| `ALMANAUT_NOTIFY_WITHIN_DAYS` | `30`                 | Days ahead to treat certificates/warranties/renewals as "expiring soon" |
| `ALMANAUT_NOTIFY_INTERVAL`    | `24h`                | How often the notifier checks (Go duration, e.g. `12h`) |
| `ALMANAUT_WEBHOOKS_ENABLED`   | `false`              | Master switch for outbound webhooks; disabled leaves delivery off |
| `ALMANAUT_WEBHOOK_TIMEOUT`    | `10s`                | Per-delivery HTTP timeout for webhook requests (Go duration, e.g. `5s`) |
| `ALMANAUT_WEBHOOK_MAX_ATTEMPTS` | `5`                | Delivery attempts (with backoff) before giving up and logging the drop |
| `ALMANAUT_KUMA_URL`           | (empty)              | Uptime Kuma base URL (e.g. `http://kuma.lan:3001`); enables the monitor sync when set together with user and pass |
| `ALMANAUT_KUMA_USER`          | (empty)              | Kuma username (socket.io login; API keys don't cover monitor CRUD) |
| `ALMANAUT_KUMA_PASS`          | (empty)              | Kuma password (supports the `_FILE` convention) |
| `ALMANAUT_KUMA_INSECURE`      | `false`              | Skip TLS verification for a self-signed Kuma certificate |
| `ALMANAUT_LIVENESS_ENABLED`   | `false`              | Master switch for native TCP liveness checks on hosts/services (per-entity check address; empty address = not monitored) |
| `ALMANAUT_LIVENESS_INTERVAL`  | `60s`                | How often the liveness checker runs (Go duration, e.g. `30s`) |
| `ALMANAUT_LIVENESS_TIMEOUT`   | `5s`                 | Per-address TCP dial timeout for liveness checks (Go duration) |
| `ALMANAUT_CERT_PROBE_ENABLED` | `false`              | Master switch for the scheduled certificate-probing job; the per-cert "Probe now" button works regardless |
| `ALMANAUT_CERT_PROBE_INTERVAL` | `24h`               | How often the scheduled cert-probe job runs (Go duration) |
| `ALMANAUT_CERT_PROBE_TIMEOUT` | `10s`                | Per-endpoint TLS dial timeout when probing a certificate (Go duration) |
| `ALMANAUT_DISCOVERY_DOCKER_INTERVAL` | (unset)       | Interval for scheduled Docker discovery (Go duration, e.g. `1h`); unset/0 disables it. Findings surface as proposals on the Discovery page — nothing is auto-imported |
| `ALMANAUT_DISCOVERY_NETWORK_INTERVAL` | (unset)      | Interval for scheduled network discovery (Go duration); also requires the network scan enabled and a subnet set; unset/0 disables it |
| `ALMANAUT_DISCOVERY_PROXMOX_INTERVAL` | (unset)      | Interval for scheduled Proxmox discovery (Go duration); also requires Proxmox configured; unset/0 disables it |

### Secrets from files

Sensitive values (`ALMANAUT_AUTH_PASS`, `ALMANAUT_PROXMOX_TOKEN`,
`ALMANAUT_NTFY_TOKEN`, `ALMANAUT_DISCORD_WEBHOOK_URL`, `ALMANAUT_KUMA_PASS`)
can instead be read from a file by appending `_FILE` to the variable name and
pointing it at the file (`ALMANAUT_AUTH_PASS_FILE=/run/secrets/auth_pass`).
This keeps the secret out of the process environment, where it would otherwise
be visible via `docker inspect`, `/proc`, or inherited by child processes. It
pairs directly with [Docker secrets](https://docs.docker.com/engine/swarm/secrets/)
and Kubernetes secrets, which are mounted as files. The `_FILE` variant takes
precedence over the plain variable, and a single trailing newline is stripped.

### Behind a reverse proxy (TLS)

Almanaut serves plain HTTP with no built-in TLS, so put a reverse proxy in
front for anything beyond localhost, and set `ALMANAUT_SECURE_COOKIES=true` so
cookies get the `Secure` flag once TLS is terminated upstream.

**Caddy** — automatic Let's Encrypt TLS in two lines (`Caddyfile`):

```caddyfile
almanaut.example.com {
    reverse_proxy almanaut:8080
}
```

**Traefik** — as labels on the `almanaut` service in your `docker-compose.yml`:

```yaml
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.almanaut.rule=Host(`almanaut.example.com`)"
      - "traefik.http.routers.almanaut.entrypoints=websecure"
      - "traefik.http.routers.almanaut.tls.certresolver=le"
      - "traefik.http.services.almanaut.loadbalancer.server.port=8080"
```

### Security model

Almanaut is built for a **trusted LAN behind a reverse proxy or VPN**, not for
direct internet exposure:

- Session cookies and login forms travel over plain HTTP until you terminate
  TLS in front of it.
- Login throttling slows brute force but there is no CAPTCHA, 2FA, or
  IP-level rate limiting.
- `/export` returns the **entire inventory**, including account entries
  (usernames, password-manager names, and secret references) — any logged-in
  user (including viewers) can download it, so treat every account you create
  as having read access to all of it.
- The change history is append-only and **retains prior values of edited
  fields** — including account fields such as `username` and `secret_ref`. A
  value you later change is not scrubbed from history. (`secret_ref` is a
  pointer to where a secret lives, not a stored secret.)

## The inventory model

Fifteen entity types, all sharing the same machinery — search, tags,
relationships, change history, journal, custom fields, attachments, the JSON
API, and CSV import:

| | |
|---|---|
| **Hosts** | Physical machines, VMs, LXC containers, and VPSes |
| **Services** | The things running on your hosts |
| **Networks** | Subnets, with built-in IPAM (usage, capacity, next-free IP) |
| **Domains** | DNS names / FQDNs |
| **Certificates** | TLS certs with expiry tracking |
| **Backups** | What's backed up, from where |
| **Hardware** | Devices with warranty tracking |
| **Subscriptions** | Recurring services with renewal dates |
| **Accounts** | Logins and secret references |
| **Sites / Locations / Racks** | Physical placement hierarchy (see below) |
| **Contacts** | People and vendors responsible for infrastructure |
| **VLANs** | 802.1Q VLANs referenced by networks |
| **IP reservations** | Named reserved ranges within a network |

### Sites, locations & racks

Physical placement is a **Site → Location → Rack** hierarchy: a site is a
building or campus, a location is a room or area within it, and a rack has a
height in rack units (U). Each level's detail page lists its children, so you
can navigate top-down.

Hosts and hardware can be **assigned to a rack and a U position** (with a
height in U) from their edit form. The rack's detail page then renders a
**U elevation** — a top-to-bottom diagram with each occupant drawn at its
position, linking to its detail page. Occupants that extend past the rack or
overlap another are highlighted; placement is advisory, not enforced at save
time.

### IPAM: VLANs & IP reservations

A network can reference a **VLAN** (name + 802.1Q ID) from its edit form; the
network's detail page shows the resolved `VLAN <id> (<name>)`.

**IP reservations** mark a named range within a network (a DHCP pool, a block
kept for switches). Reserved addresses show on the network's IPAM view, are
skipped by the "next free" suggestion, and are subtracted from the
free-address count.

### Custom fields

Admins define **custom fields** at `/custom-fields`: each field belongs to one
entity type and has a kind (text, number, bool, or date). Defined fields
appear on that type's edit forms and detail pages, their values are searchable
from global search, and they round-trip through the YAML export.

### Attachments

Any entity's detail page accepts **file attachments** (manuals, invoices,
config dumps) up to 16 MiB each, stored inside the SQLite database.
Attachments are **not** included in the YAML export — back up the database
file itself to keep them.

### History & journal

Every create, update, and delete is recorded automatically with a field-level
diff (e.g. `status: running → down`). Each entity's detail page shows its
**Journal** — manual, categorised notes (info / success / warning / incident)
you add as a running log — and a collapsible **Change history**. A global
**`/history`** feed (and a "Recent activity" panel on the dashboard) lists the
latest changes across every entity; delete events remain visible there even
after the entity is gone. Journal entries are included in the YAML export;
the change log is not.

## Auto-discovery

All discovery sources are read-only and additive: they only ever **create**
new records and never overwrite your manual data.

### Docker containers

Mount the Docker socket read-only into the container:

```bash
docker run --rm -p 8080:8080 -v almanaut-data:/data \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/dealisto/almanaut:dev
```

(Override a non-standard socket path with `ALMANAUT_DOCKER_SOCKET`.)

Then navigate to **Discover → Docker containers**. Optionally select a host,
choose the containers to import, and they are created as Services with an
automatic "runs on" relationship to the selected host.

### Network scan

Set `ALMANAUT_ENABLE_NETWORK_SCAN=true` (and optionally `ALMANAUT_SCAN_SUBNET`
to pre-fill the form). Then navigate to **Discover → Network scan**, enter a
subnet (CIDR) and optional ports, and import the live hosts you select. The
scan is a lightweight pure-Go TCP-connect probe (a host is "live" if at least
one probed port is open). Subnets larger than 1024 hosts are rejected.

### Proxmox VE

Set `ALMANAUT_PROXMOX_URL` (e.g. `https://pve.lan:8006`) and
`ALMANAUT_PROXMOX_TOKEN`. The token needs read access; to create one:

1. In the Proxmox web UI, go to **Datacenter → Permissions → API Tokens**
2. Click **Add**, choose a user and token ID
3. Assign the **PVEAuditor** role (or an equivalent read-only role)
4. Copy the token as `user@realm!tokenid=secret` into `ALMANAUT_PROXMOX_TOKEN`

For a self-signed Proxmox certificate (the default), set
`ALMANAUT_PROXMOX_INSECURE=true`.

Then navigate to **Discover → Proxmox**, review the discovered resources,
optionally keep "Link VMs/LXC to their Proxmox node" checked to create
"runs on" relationships, and import. Proxmox nodes become `physical` hosts,
QEMU VMs become `vm` hosts, and LXC containers become `lxc` hosts.

## Notifications & integrations

### Expiry notifications (ntfy & Discord)

Set `ALMANAUT_NTFY_URL` to an [ntfy](https://ntfy.sh) topic URL and/or
`ALMANAUT_DISCORD_WEBHOOK_URL` to a Discord
[incoming-webhook](https://support.discord.com/hc/en-us/articles/228383668)
URL. Almanaut pushes an alert when a certificate, hardware warranty, or
subscription renewal falls within `ALMANAUT_NOTIFY_WITHIN_DAYS` (default 30).

Each item notifies **once** per configured channel; renewing it (pushing the
date beyond the window) re-arms it for next time. The check runs at startup
and every `ALMANAUT_NOTIFY_INTERVAL`. Leave both URLs unset to disable
notifications entirely.

### Outbound webhooks

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

### Uptime Kuma sync

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

## Export & import

The whole inventory round-trips through a single YAML file. **Data → Export**
(or `GET /export`) downloads `almanaut-export.yaml`; **Data → Import** uploads
one back. This is your backup/restore and migration path (attachments
excepted — they live only in the database file).

> ⚠️ Import **replaces the entire inventory** — every existing record is
> deleted and re-created from the file. It is not a merge. The import form
> makes you tick a confirmation checkbox first.

### Additive CSV import

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

## JSON API

A read-write JSON API mirrors the inventory for scripts and dashboards.
**Reads** (`GET`) accept either a logged-in session cookie or an API token.
**Writes** (`POST`/`PUT`/`DELETE`) require an API token — a request carrying
only a session cookie is rejected, so a browser can never trigger a write via
a plain form post. Any request missing valid credentials gets a plain
`401 {"error":"…"}` instead of the UI's redirect to `/login`.

### API tokens

Create a personal token at **API tokens** (`/account/tokens`) while logged in:
give it a label and a **scope** — `read-write` or `read-only`. A request's
effective permission is the intersection of the token's scope and its owner's
role (a viewer's token can never write, and a read-only token can't write
even for an admin).

The raw token (`alm_...`) is shown **once**, right after creation — copy it
then, since only its hash is stored. Revoke a token from the same page at any
time; each user only sees and can revoke their own tokens.

Send the token as a bearer header:

```bash
curl -X POST http://localhost:8080/api/hosts \
  -H "Authorization: Bearer alm_..." \
  -H "Content-Type: application/json" \
  -d '{"name":"nas","type":"physical"}'
```

### Endpoints

| Endpoint | Returns |
|---|---|
| `GET /api/{type}` | All entities of a type (e.g. `/api/hosts`, `/api/hardware`, `/api/certificates`) |
| `POST /api/{type}` | Create an entity from a JSON body; `201` + `Location` header + the created entity, or `400` on validation/malformed-JSON errors |
| `GET /api/{type}/{id}` | One entity, or `404 {"error":"…"}` if absent |
| `PUT /api/{type}/{id}` | Full replace from a JSON body (not a partial patch); `200` + the updated entity, or `404`/`400` |
| `DELETE /api/{type}/{id}` | `204` on success, `404` if absent |
| `GET /api/search?q=<term>` | Flat array of matches: `[{"type","id","label","path"}]` |
| `GET /api/relationships` | All relationships |
| `GET /api/openapi.json` | The OpenAPI 3 document describing every route and schema |

`{type}` bases mirror the web UI's routes, not a naive plural of the entity
name — `hardware`, not `hardwares` (see [The inventory model](#the-inventory-model)
for the full set). Field names match the YAML export (snake_case), and request
bodies use the same shape. Responses are `application/json`.

Every API write is recorded in the same per-entity **change history** as UI
edits, attributed to the token's owning user — so `GET /history` and each
entity's Change history show exactly who made a scripted change.

```bash
curl -s http://localhost:8080/api/certificates \
  -H "Authorization: Bearer alm_..." | jq '.[] | {subject, expires_on}'
```

### API reference

A browsable reference lives at **API docs** (`/api/docs`) — every endpoint and
entity schema, rendered server-side (no external JS). The same information is
served as a machine-readable [OpenAPI 3](https://spec.openapis.org/oas/v3.0.3)
document at `/api/openapi.json`, suitable for client generators or import into
tools like Postman. Both are generated from the entity catalog, so they always
match the running build.

## Metrics

`GET /metrics` exposes aggregate inventory gauges in the Prometheus text
format. It is authenticated like the JSON API: pass an API token as a bearer
token (a logged-in browser can also view it via its session cookie).

| Metric | Meaning |
|---|---|
| `almanaut_entities_total{type="…"}` | Count of each entity type |
| `almanaut_relationships_total` | Number of relationships |
| `almanaut_certificates_expiring_total` | Certificates expiring within 30 days |
| `almanaut_hardware_warranty_expiring_total` | Warranties expiring within 30 days |
| `almanaut_subscriptions_renewal_due_total` | Renewals due within 30 days |
| `almanaut_hosts_down_total` | Hosts marked down/offline/stopped |
| `almanaut_services_without_backup_total` | Services with no backup relationship |

### Prometheus scrape config

```yaml
scrape_configs:
  - job_name: almanaut
    authorization:
      credentials: alm_...        # an API token from /account/tokens
    static_configs:
      - targets: ["almanaut.example:8080"]
```

## Health & version

Two unauthenticated endpoints are always available (they bypass the login so
probes can reach them):

| Endpoint    | Response                                                        |
|-------------|-----------------------------------------------------------------|
| `/healthz`  | `200 {"status":"ok","version":"..."}` when the database is reachable, `503` otherwise |
| `/version`  | `{"version":"..."}` — the build version (`dev` for local builds) |

The Docker image ships a `HEALTHCHECK` that runs `almanaut healthcheck`, a
built-in subcommand that probes the local `/healthz` and exits non-zero when
unhealthy (the distroless image has no shell, so the binary is its own probe).

To stamp a version into a build, pass it at build time:

```bash
# from source
go build -ldflags "-X main.version=v0.2.0" -o almanaut .

# Docker
docker build --build-arg VERSION=v0.2.0 -t almanaut .
```

## License

[MIT](LICENSE)
