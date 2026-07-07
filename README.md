# Almanaut

[![CI](https://github.com/Dealisto/almanaut/actions/workflows/ci.yml/badge.svg)](https://github.com/Dealisto/almanaut/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Container image](https://img.shields.io/badge/ghcr.io-dealisto%2Falmanaut-2496ed?logo=docker&logoColor=white)](https://github.com/Dealisto/almanaut/pkgs/container/almanaut)

A lightweight, self-hosted homelab inventory & documentation tool.
"NetBox for the rest of us."

> Status: early development (v0.1).

## Contents

- [What it does](#what-it-does)
- [Screenshots](#screenshots)
- [Run with Docker](#run-with-docker)
- [Run with Docker Compose](#run-with-docker-compose)
- [Run from source](#run-from-source)
- [Authentication](#authentication)
- [Configuration](#configuration)
- [Export & import](#export--import)
- [JSON API](#json-api)
- [Metrics](#metrics)
- [Health & version](#health--version)
- [Auto-discovery](#auto-discovery)
- [License](#license)

## What it does

Almanaut is a single Go binary (SQLite storage, server-rendered UI, no
client-side JS framework) for keeping track of your homelab. It tracks nine
entity types and the relationships between them:

- **Hosts** — physical machines, VMs, LXC containers, and VPSes
- **Services** — the things running on your hosts
- **Networks** — subnets, with built-in IPAM (usage, capacity, next-free IP)
- **Domains** — DNS names / FQDNs
- **Certificates** — TLS certs with expiry tracking
- **Backups** — what's backed up, from where
- **Hardware** — devices with warranty tracking
- **Subscriptions** — recurring services with renewal dates
- **Accounts** — logins and secret references

### Sites, Locations & Racks

almanaut models physical placement as a **Site → Location → Rack** hierarchy:

- A **Site** is a building or campus.
- A **Location** is a room/area within a site.
- A **Rack** is an equipment rack within a location, with a height in rack units (U).

Each level references its parent (chosen from a dropdown). A site's detail page
lists its locations, and a location's lists its racks, so you can navigate the
hierarchy top-down. Like every entity, they support search, tags, relationships,
change history, the JSON API, and CSV import.

Hosts and hardware can be **assigned to a rack and a U position** (with a height
in U) from their edit form. The rack's detail page then renders a **U elevation**
— a top-to-bottom diagram of the rack with each occupant drawn at its position,
linking to its detail page. Occupants that extend past the rack or overlap
another are highlighted; placement is advisory, not enforced at save time.

### Contacts

**Contacts** record the people and vendors responsible for infrastructure
(name, email, phone, role, organization). Link a contact to any entity through
the relationship catalog with the **administered by** or **owned by** kinds; the
link shows on both detail pages and in the neighbourhood graph. Contacts support
search, tags, history, the JSON API, and CSV import like every other entity.

On top of the inventory you get:

- **Relationships & a neighbourhood graph** on each detail page (e.g. a service
  *runs on* a host, *is backed up by* a backup)
- **Global search** across every entity type
- **A dashboard** summarising your inventory and what's expiring soon
- **Auto-discovery** from Docker, a network subnet scan, and Proxmox VE
- **History & journal** — an automatic per-entity change log (field-level
  diffs), manual categorised journal entries on each detail page, and a global
  `/history` activity feed (also surfaced on the dashboard)
- **Expiry notifications** via [ntfy](https://ntfy.sh) for certs, warranties,
  and renewals
- **A read-write JSON API** with per-user API tokens, and a Prometheus
  **`/metrics`** endpoint
- **YAML export/import** of the entire inventory
- **Mandatory account-based login** — session-cookie auth with per-user
  passwords, an auto-created admin on first run, and a `/users` admin page

## Screenshots

**Dashboard** — a launchpad: prominent search, a "needs attention" panel with
severity dots, clickable service tiles, and a compact inventory strip, all on a
grouped left sidebar:

![almanaut dashboard](docs/screenshots/dashboard.png)

**Relationship graph** — each entity's neighbourhood is drawn on its detail
page (here, a host with the VM it runs on and the services running on it):

![relationship neighbourhood graph](docs/screenshots/graph.png)

**Dark mode** — a built-in System / Light / Dark switch; System follows your OS:

![almanaut in dark mode](docs/screenshots/dashboard-dark.png)

## Run with Docker

```bash
docker run --rm -p 8080:8080 -v almanaut-data:/data ghcr.io/dealisto/almanaut:dev
```

Then open http://localhost:8080.

Images are published to GHCR automatically: `:dev` tracks `master`, and a
tagged release (`vX.Y.Z`) publishes `:X.Y.Z`, `:X.Y`, and `:latest`. Images are
multi-arch (`linux/amd64` and `linux/arm64`).

The container runs as a non-root user (uid `65532`). A fresh named volume (as
above) inherits the right ownership automatically. If you instead bind-mount a
host directory (`-v /host/path:/data`), make it writable by that uid first —
`sudo chown 65532:65532 /host/path` — otherwise the database cannot be created.

## Run with Docker Compose

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

Then open http://localhost:8080. The image ships its own `HEALTHCHECK`, so
`docker compose ps` reports the container's health directly.

## Run from source

```bash
go build -o almanaut .
ALMANAUT_DATA_DIR=./data ./almanaut
```

## Authentication

Almanaut **requires a login** — every page and the whole JSON API sit behind a
session-cookie auth check, with no way to run it open. (This is a breaking
change from earlier versions' optional HTTP Basic auth.)

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

`ALMANAUT_AUTH_USER` / `ALMANAUT_AUTH_PASS` only seed that *initial* admin;
they are **not** HTTP Basic auth credentials and are not checked on every
request. The `_FILE` convention still applies to `ALMANAUT_AUTH_PASS` (see
[Secrets from files](#secrets-from-files) below).

Locked out? Set `ALMANAUT_RESET_ADMIN=true` and restart — almanaut resets the
admin's password (using `ALMANAUT_AUTH_PASS` if set, otherwise a fresh random
one) and logs the new value the same way. **Unset `ALMANAUT_RESET_ADMIN`
afterwards**, or every restart will reset the password again.

Once logged in, manage accounts at **Users** (`/users`): create, list, delete,
and reset other users' passwords. The last remaining user cannot be deleted,
so you can't lock yourself out entirely. Each user changes their own password
at `/account/password`. There are no roles yet — every logged-in user has full
access.

Sessions are server-side (stored in SQLite), cookie-based, and last 30 days;
use the **Logout** button to end one early. For scripted/programmatic access,
create a personal API token at **API tokens** (`/account/tokens`) — see
[JSON API](#json-api). `/api/*` returns a plain `401` JSON error when called
without valid credentials (a session cookie or a bearer token). `/healthz` and
`/version` are the only endpoints that bypass the login, so container health
probes keep working.

Because credentials and the session cookie travel in the request just like
before, put a TLS-terminating reverse proxy in front of almanaut for anything
beyond localhost (see [Behind a reverse proxy](#behind-a-reverse-proxy-tls))
and set `ALMANAUT_SECURE_COOKIES=true` once you do.

## Configuration

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
| `ALMANAUT_NOTIFY_WITHIN_DAYS` | `30`                 | Days ahead to treat certificates/warranties/renewals as "expiring soon" |
| `ALMANAUT_NOTIFY_INTERVAL`    | `24h`                | How often the notifier checks (Go duration, e.g. `12h`) |

### Secrets from files

The two sensitive values — `ALMANAUT_AUTH_PASS` and `ALMANAUT_PROXMOX_TOKEN` —
can instead be read from a file by appending `_FILE` to the variable name and
pointing it at the file (`ALMANAUT_AUTH_PASS_FILE=/run/secrets/auth_pass`). This
keeps the secret out of the process environment, where it would otherwise be
visible via `docker inspect`, `/proc`, or inherited by child processes. It pairs
directly with [Docker secrets](https://docs.docker.com/engine/swarm/secrets/) and
Kubernetes secrets, which are mounted as files. The `_FILE` variant takes
precedence over the plain variable, and a single trailing newline is stripped.

`ALMANAUT_AUTH_USER` / `ALMANAUT_AUTH_PASS` only seed the initial admin account
at first startup — see [Authentication](#authentication) for the full login
model, the bootstrap banner, and `ALMANAUT_RESET_ADMIN`. Every page requires a
logged-in session, and the JSON API requires either a session cookie or an API
token (see [JSON API](#json-api)); there is no unauthenticated mode.
Session cookies and login form submissions travel like any other request, so
terminate TLS in front of Almanaut when exposing it beyond localhost. Almanaut
also does not rate-limit or lock out failed logins, so do not expose it
directly to the internet — keep it behind a reverse proxy (which can add
throttling and TLS) or a VPN.

Note that `/export` returns the **entire inventory**, including account entries
(usernames, password-manager names, and secret references) — anyone with a
valid login can download it, so treat every account you create as having full
access to that data.

### Behind a reverse proxy (TLS)

Almanaut serves plain HTTP with no built-in TLS or rate limiting, so put a
reverse proxy in front for anything beyond localhost. Whatever proxy you use,
set `ALMANAUT_SECURE_COOKIES=true` so cookies get the `Secure` flag once TLS is
terminated upstream.

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

### Expiry notifications

Set `ALMANAUT_NTFY_URL` to an [ntfy](https://ntfy.sh) topic URL and almanaut
pushes a notification when a certificate, hardware warranty, or subscription
renewal falls within `ALMANAUT_NOTIFY_WITHIN_DAYS` (default 30). Each item
notifies **once**; renewing it (pushing the date beyond the window) re-arms it
for next time. The check runs at startup and every `ALMANAUT_NOTIFY_INTERVAL`.
Leave `ALMANAUT_NTFY_URL` unset to disable notifications entirely.

## Export & import

The whole inventory round-trips through a single YAML file. **Data → Export**
(or `GET /export`) downloads `almanaut-export.yaml`; **Data → Import** uploads
one back. This is your backup/restore and migration path.

> ⚠️ Import **replaces the entire inventory** — every existing record is
> deleted and re-created from the file. It is not a merge. The import form
> makes you tick a confirmation checkbox first.

### Try it with sample data

Want to see the app populated before entering your own data? This repo ships a
small example homelab (three hosts, some services, a network, a certificate, a
backup, and the relationships between them). Grab
[`examples/inventory.yaml`](examples/inventory.yaml), then go to **Data →
Import**, upload it, tick the confirmation box, and import. You'll land on a
populated dashboard with a browsable relationship graph.

Since import wipes existing data, only load the sample into a fresh instance
(or export your real data first).

### Additive CSV import

The **Data** page also imports a CSV for a single entity type without touching
any other data — the complement to the whole-inventory YAML import (which
replaces everything).

- The header row uses the entity's field names in `snake_case`, matching the
  YAML export (e.g. for hosts: `name,type,os,cpu,ram,disk,status,ips,notes`).
- An optional `id` column controls create vs. update: a row with an existing
  `id` **updates** that row; a blank or absent `id` **creates** a new row.
- Multi-value fields use commas inside the cell — e.g. a host's `ips` column:
  `"10.0.0.1,10.0.0.2"` (quote the cell so the commas are not column separators).
- Boolean fields (`auto_renew`) accept `true`/`false` (also `1`/`0`, `yes`/`no`).
- Updating a row is a **full replace** of that row's columns present in the
  file — an omitted column is written as empty, same as clearing it in the edit
  form.
- The import is **all-or-nothing**: if any row is invalid, the page lists every
  bad row and writes nothing. Each created/updated row is recorded in the
  entity's history.

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
a plain form post (no separate CSRF token is needed for `/api`, since a
browser never sends an `Authorization` header on its own). Any request
missing valid credentials gets a plain `401 {"error":"…"}` instead of the
UI's redirect to `/login`.

### API tokens

Create a personal token at **API tokens** (`/account/tokens`) while logged
in: give it a label and it's created immediately. The raw token (`alm_...`)
is shown **once**, right after creation — copy it then, since only its hash
is stored and it cannot be redisplayed. Revoke a token from the same page at
any time; each user only sees and can revoke their own tokens. There are no
per-token scopes yet — a token carries its owner's full access, same as their
session would.

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

`{type}` bases mirror the web UI's routes, not a naive plural of the entity
name — `hardware`, not `hardwares` (see the entity list under
[What it does](#what-it-does) for the full set). Field names match the YAML
export (snake_case), and request bodies use the same shape. Responses are
`application/json`.

Every API write is recorded in the same per-entity **change history** as UI
edits (see [History & journal](#history--journal)), attributed to the
token's owning user — so `GET /history` and each entity's Change history show
exactly who made a scripted change, same as a UI edit.

```bash
curl -s http://localhost:8080/api/certificates | jq '.[] | {subject, expires_on}'
```

## Metrics

`GET /metrics` exposes aggregate inventory gauges in the Prometheus text format.
When authentication is enabled, `/metrics` is authenticated like the JSON API:
create an API token at **API tokens** (`/account/tokens`) and pass it as a
bearer token. A logged-in browser can also view it via its session cookie.

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

When authentication is disabled, the endpoint is open:

```bash
curl -s http://localhost:8080/metrics
```

## History & journal

Every create, update, and delete is recorded automatically with a field-level
diff (e.g. `status: running → down`). Each entity's detail page shows its
**Journal** — manual, categorised notes (info / success / warning / incident)
you add as a running log — and a collapsible **Change history**. A global
**`/history`** feed (and a "Recent activity" panel on the dashboard) lists the
latest changes across every entity; delete events remain visible there even
after the entity is gone. Journal entries are included in the YAML export;
the change log is not.

## Health & version

Two unauthenticated endpoints are always available (they bypass the login so
probes can reach them):

| Endpoint    | Response                                                        |
|-------------|-----------------------------------------------------------------|
| `/healthz`  | `200 ok` when the database is reachable, `503` otherwise         |
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

## Auto-discovery

To enable Docker container discovery, mount the Docker socket read-only into the container:

```bash
docker run --rm -p 8080:8080 -v almanaut-data:/data -v /var/run/docker.sock:/var/run/docker.sock:ro ghcr.io/dealisto/almanaut:dev
```

If your Docker socket is at a non-standard path, override it with the `ALMANAUT_DOCKER_SOCKET` environment variable.

Then navigate to **Discover → Docker containers** to scan for containers. Optionally select a host, choose the containers you want to import, and they will be created as Services with an automatic "runs on" relationship to the selected host. Discovery only reads from the socket and creates new Services—it never overwrites your manual data.

### Network scan

To enable network subnet scanning, set `ALMANAUT_ENABLE_NETWORK_SCAN=true`. Optionally set `ALMANAUT_SCAN_SUBNET` to pre-fill the subnet in the scan form.

Then navigate to **Discover → Network scan**, enter a subnet (CIDR) and optional ports, click Scan, pick a host type, select the hosts you want to import, and they will be created. The scan is a lightweight pure-Go TCP-connect probe (a host is considered "live" if at least one probed port is open). Network discovery only ever creates new Hosts and never overwrites your manual data. Subnets larger than 1024 hosts are rejected.

### Proxmox

To enable Proxmox discovery, set both `ALMANAUT_PROXMOX_URL` (e.g. `https://pve.lan:8006`) and `ALMANAUT_PROXMOX_TOKEN`. The token must be a Proxmox API token with read access. To create one:

1. Log in to your Proxmox VE web interface
2. Navigate to **Datacenter → Permissions → API Tokens**
3. Click **Add**
4. Choose a user (e.g. `root`) and token ID
5. Assign the **PVEAuditor** role (or equivalent read-only role) to the token
6. Copy the token in the format `user@realm!tokenid=secret` and set it as `ALMANAUT_PROXMOX_TOKEN`

If your Proxmox server uses a self-signed certificate (the default), set `ALMANAUT_PROXMOX_INSECURE=true` to skip TLS verification.

Then navigate to **Discover → Proxmox**, review the discovered resources, optionally keep "Link VMs/LXC to their Proxmox node" checked to create "runs on" relationships, select what to import, and click Import. Proxmox nodes are imported as `physical` hosts, QEMU VMs as `vm` hosts, and LXC containers as `lxc` hosts. Proxmox discovery only reads and only ever creates new Hosts—it never overwrites your manual data.

## License

MIT (see LICENSE).
