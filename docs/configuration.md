[← Documentation index](README.md) · [almanaut](../README.md)

# Configuration

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
| `ALMANAUT_DISCOVERY_DOCKER_INTERVAL` | (unset)       | Interval for scheduled Docker discovery (Go duration, e.g. `1h`); unset/0 disables it. Findings surface as proposals on the Discovery page — nothing is auto-imported |
| `ALMANAUT_DISCOVERY_NETWORK_INTERVAL` | (unset)      | Interval for scheduled network discovery (Go duration); also requires the network scan enabled and a subnet set; unset/0 disables it |
| `ALMANAUT_DISCOVERY_PROXMOX_INTERVAL` | (unset)      | Interval for scheduled Proxmox discovery (Go duration); also requires Proxmox configured; unset/0 disables it |
| `ALMANAUT_STALE_AFTER_DAYS`   | `90`                 | Staleness window (days) for the inventory-health stale-entity rule; `0` disables that rule |

## Secrets from files

Sensitive values (`ALMANAUT_AUTH_PASS`, `ALMANAUT_PROXMOX_TOKEN`,
`ALMANAUT_NTFY_TOKEN`, `ALMANAUT_DISCORD_WEBHOOK_URL`, `ALMANAUT_KUMA_PASS`)
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

**See also:** [Installation](installation.md) · [Authentication & access control](authentication.md) · [Notifications & integrations](integrations.md)
