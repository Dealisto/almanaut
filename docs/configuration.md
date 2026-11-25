[← Documentation index](README.md) · [almanaut](../README.md)

# Configuration

Configuration uses environment variables, with an optional JSON file for
multiple discovery sources. The variables that schedule recurring work —
liveness, certificate probing and discovery — are covered in more depth in
[Background jobs](background-jobs.md).

| Variable                      | Default              | Description                                    |
|-------------------------------|----------------------|------------------------------------------------|
| `ALMANAUT_ADDR`               | `:8080`              | TCP listen address                             |
| `ALMANAUT_DATA_DIR`           | `./data`             | Directory for the SQLite database              |
| `ALMANAUT_DISCOVERY_SOURCES_FILE` | (empty) | JSON file defining multiple engine/hypervisor sources; replaces the single-source settings below when set |
| `ALMANAUT_DOCKER_SOCKET`      | `/var/run/docker.sock` | Path to the Docker or Podman compatibility socket for auto-discovery; see [Podman](podman.md) |
| `ALMANAUT_ENABLE_NETWORK_SCAN` | `false`              | Enable the opt-in subnet scan                  |
| `ALMANAUT_SCAN_SUBNET`        | (empty)              | Default subnet (CIDR) pre-filled in the scan form |
| `ALMANAUT_PROXMOX_URL`        | (empty)              | Proxmox VE API base URL (e.g. `https://pve.lan:8006`); enables Proxmox discovery when set with a token |
| `ALMANAUT_PROXMOX_TOKEN`      | (empty)              | Proxmox API token (`user@realm!tokenid=secret`) |
| `ALMANAUT_PROXMOX_INSECURE`   | `false`              | Skip TLS verification for a self-signed Proxmox certificate |
| `ALMANAUT_XO_URL` | (empty) | Xen Orchestra base URL (e.g. `https://xo.lan`); enables XCP-ng discovery when set with a token |
| `ALMANAUT_XO_TOKEN` | (empty) | Xen Orchestra authentication token; supports `_FILE` |
| `ALMANAUT_XO_INSECURE` | `false` | Skip TLS verification for a self-signed Xen Orchestra certificate |
| `ALMANAUT_AUTH_USER`          | `admin`              | Seeds the username of the initial admin account created on first startup |
| `ALMANAUT_AUTH_PASS`          | (empty)              | Seeds the password of the initial admin account; a random password is generated and logged once when unset |
| `ALMANAUT_RESET_ADMIN`        | `false`              | Reset the admin password at startup (lockout recovery) and log the new value; unset it again afterwards |
| `ALMANAUT_SECURE_COOKIES`     | `false`              | Force the `Secure` flag on cookies; set to `true` when serving HTTPS through a TLS-terminating reverse proxy |
| `ALMANAUT_PROXY_AUTH_HEADER`  | (empty)              | Identity header set by a trusted reverse proxy (e.g. `Remote-User`), enabling SSO; empty disables header auth |
| `ALMANAUT_PROXY_AUTH_ALLOWLIST` | (empty)            | Comma-separated proxy IPs/CIDRs the identity header is trusted from |
| `ALMANAUT_PROXY_AUTH_AUTOPROVISION` | `false`        | Create a local user the first time an unknown proxy identity authenticates |
| `ALMANAUT_PROXY_AUTH_DEFAULT_ROLE` | `viewer`        | Role given to auto-provisioned SSO users |
| `ALMANAUT_AUTH_AUDIT_RETENTION_DAYS` | `90`          | Days to keep authentication audit events; `0` keeps them forever |
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
| `ALMANAUT_DISCOVERY_DOCKER_INTERVAL` | (unset)       | Interval for scheduled Docker / Podman discovery (Go duration, e.g. `1h`); unset/0 disables it. Findings surface as proposals on the Discovery page — nothing is auto-imported |
| `ALMANAUT_DISCOVERY_NETWORK_INTERVAL` | (unset)      | Interval for scheduled network discovery (Go duration); also requires the network scan enabled and a subnet set; unset/0 disables it |
| `ALMANAUT_DISCOVERY_PROXMOX_INTERVAL` | (unset)      | Interval for scheduled Proxmox discovery (Go duration); also requires Proxmox configured; unset/0 disables it |
| `ALMANAUT_DISCOVERY_XO_INTERVAL` | (unset) | Interval for scheduled Xen Orchestra discovery (Go duration); requires XO URL and token; unset/0 disables it |
| `ALMANAUT_STALE_AFTER_DAYS`   | `90`                 | Staleness window (days) for the inventory-health stale-entity rule; `0` disables that rule |

On Unix, new data directories use owner-only permissions (`0700`), and new
database files and pre-migration backups use `0600`. Existing directory,
database, and backup permissions are preserved.

## Multiple discovery sources

Set `ALMANAUT_DISCOVERY_SOURCES_FILE` to a JSON file readable by Almanaut:

```bash
export ALMANAUT_DISCOVERY_SOURCES_FILE=/etc/almanaut/discovery-sources.json
```

For example:

```json
[
  {
    "id": "rootless",
    "name": "Application containers",
    "type": "podman",
    "socket": "/run/user/1000/podman/podman.sock",
    "interval": "1h"
  },
  {
    "id": "lab",
    "name": "Lab Proxmox",
    "type": "proxmox",
    "url": "https://pve-lab.example.test:8006",
    "token_file": "secrets/pve-lab-token",
    "interval": "6h"
  },
  {
    "id": "office",
    "name": "Office Proxmox",
    "type": "proxmox",
    "url": "https://pve-office.example.test:8006",
    "token_file": "secrets/pve-office-token"
  },
  {
    "id": "test",
    "name": "Test Xen Orchestra",
    "type": "xen-orchestra",
    "url": "https://xo.example.test",
    "token_file": "secrets/xo-token",
    "interval": "6h"
  }
]
```

| Field | Meaning |
|---|---|
| `id` | Required stable identifier: 1–64 lowercase letters, digits, underscores or hyphens; starts with a letter or digit |
| `type` | Required: `docker`, `podman`, `proxmox` or `xen-orchestra` |
| `name` | Optional display label; defaults to `id` |
| `socket` | Required absolute Unix-socket path for Docker/Podman, as seen inside Almanaut's filesystem |
| `url` | Required HTTP(S) base URL for Proxmox/XO; must not contain credentials, query parameters or a fragment |
| `token_file` | File containing the hypervisor token; relative paths resolve from the source file's directory |
| `token` | Alternative inline hypervisor token; `token_file` takes precedence when both are set |
| `insecure` | Optional hypervisor TLS-verification bypass, default `false` |
| `interval` | Optional Go duration, such as `1h`; omitted or `0` disables scheduled runs while keeping manual discovery available |

Use `token_file` to keep tokens out of the source configuration. Trailing
newlines are removed from token files. An unreadable file fails startup; it does
not fall back to an inline token. Docker/Podman sources do not accept URL,
token or TLS fields; hypervisor sources do not accept a socket.

Each `type:id` pair must be unique. It is the persistent namespace for imported
resource identities, so **keep `type` and `id` stable** when changing a display
label, moving an endpoint, or rotating its token. Do not reuse the same key for
an unrelated engine or cluster. JSON order and `name` do not affect identity.
Source configuration is loaded at startup; restart Almanaut after changing it.
Unknown fields, invalid values, duplicate source keys and files larger than
1 MiB are rejected instead of partially enabling sources.

When this variable is set, the file **replaces** the legacy Docker/Podman,
Proxmox and XO connection and interval settings. `[]` explicitly disables those
sources. Network discovery remains controlled by its existing environment
variables. Without a source file, the legacy settings produce these keys:

| Legacy configuration | Source key | Review route |
|---|---|---|
| `ALMANAUT_DOCKER_SOCKET` | `docker:default` | `/discovery/docker` |
| `ALMANAUT_PROXMOX_URL` plus token | `proxmox:default` | `/discovery/proxmox` |
| `ALMANAUT_XO_URL` plus token | `xen-orchestra:default` | `/discovery/xen-orchestra` |

When moving an existing single-source setup into a file, retain the matching
`type` and `id: "default"` to preserve its associations and routes. In particular,
a Podman engine previously reached through `ALMANAUT_DOCKER_SOCKET` keeps
`type: "docker"` and `id: "default"`; the connector still speaks Podman's
compatible API. Other sources use `/discovery/sources/<type>/<id>`.

## Secrets from files

Sensitive values (`ALMANAUT_AUTH_PASS`, `ALMANAUT_PROXMOX_TOKEN`,
`ALMANAUT_XO_TOKEN`, `ALMANAUT_NTFY_TOKEN`, `ALMANAUT_DISCORD_WEBHOOK_URL`, `ALMANAUT_KUMA_PASS`)
can instead be read from a file by appending `_FILE` to the variable name and
pointing it at the file (`ALMANAUT_AUTH_PASS_FILE=/run/secrets/auth_pass`).
This keeps the secret out of the process environment, where it would otherwise
be visible via `docker inspect`, `/proc`, or inherited by child processes. It
pairs directly with [Docker secrets](https://docs.docker.com/engine/swarm/secrets/)
and Kubernetes secrets, which are mounted as files. The `_FILE` variant takes
precedence over the plain variable, and a single trailing newline is stripped.

## Behind a reverse proxy (TLS)

Almanaut serves plain HTTP with no built-in TLS, so put a reverse proxy in
front for anything beyond localhost, and set `ALMANAUT_SECURE_COOKIES=true` so
cookies get the `Secure` flag once TLS is terminated upstream.

Preserve the browser's original `Host` header when proxying requests. Form
submissions check browser origin metadata as well as CSRF tokens; a proxy that
rewrites `Host` can cause valid submissions to be rejected. HTTPS termination
at the proxy is supported without trusting forwarded origin headers.

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

## Security model

Almanaut is built for a **trusted LAN behind a reverse proxy or VPN**, not for
direct internet exposure:

- Session cookies and login forms travel over plain HTTP until you terminate
  TLS in front of it.
- Login throttling slows brute force, and each user can enable TOTP
  two-factor authentication on their account (see
  [Authentication & access control](authentication.md)), but there is no
  CAPTCHA and no IP-level rate limiting.
- [`/export`](export-import.md) returns the **entire inventory**, including account entries
  (usernames, password-manager names, and secret references) — any logged-in
  user (including viewers) can download it, so treat every account you create
  as having read access to all of it.
- The change history is append-only and **retains prior values of edited
  fields** — including account fields such as `username` and `secret_ref`. A
  value you later change is not scrubbed from history. (`secret_ref` is a
  pointer to where a secret lives, not a stored secret.)

---

**See also:** [Installation](installation.md) · [Authentication & access control](authentication.md) · [Background jobs](background-jobs.md) · [Notifications & integrations](integrations.md)
