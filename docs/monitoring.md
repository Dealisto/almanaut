[← Documentation index](README.md) · [almanaut](../README.md)

# Monitoring: metrics, health & version

The endpoints almanaut exposes for scraping and probing.

## Metrics

`GET /metrics` exposes aggregate inventory gauges in the Prometheus text
format. It is authenticated like the JSON API: pass an
[API token](api.md#api-tokens) as a bearer token (a logged-in browser can also
view it via its session cookie).

| Metric | Meaning |
|---|---|
| `almanaut_entities_total{type="…"}` | Count of each entity type |
| `almanaut_relationships_total` | Number of relationships |
| `almanaut_certificates_expiring_total` | Certificates expiring within 30 days |
| `almanaut_hardware_warranty_expiring_total` | Warranties expiring within 30 days |
| `almanaut_subscriptions_renewal_due_total` | Renewals due within 30 days |
| `almanaut_hosts_down_total` | Hosts marked down/offline/stopped |
| `almanaut_services_without_backup_total` | Services with no backup relationship |

## Prometheus scrape config

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

---

**See also:** [JSON API](api.md) · [Configuration](configuration.md)
