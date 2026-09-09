[← almanaut](../README.md)

# Documentation

| Page | What's in it |
|---|---|
| [Installation](installation.md) | Docker, Compose, prebuilt binaries, from source, first login, sample data |
| [Configuration](configuration.md) | Every environment variable, secrets from files, reverse proxy & TLS, the security model |
| [Authentication & access control](authentication.md) | Roles, sessions, login throttling, lockout recovery, TOTP 2FA, reverse-proxy SSO, the audit log |
| [The inventory model](inventory-model.md) | The 15 entity types, sites/locations/racks, IPAM, custom fields, attachments, history |
| [Lists, saved views & bulk editing](lists-and-views.md) | Filtering and sorting, saved views, bulk actions on list pages |
| [Inventory health](inventory-health.md) | The health report and its audit rules, stale entities, the checks page, impact analysis |
| [Auto-discovery](discovery.md) | Docker containers, network scan, Proxmox VE |
| [Inventory agent](agent.md) | Installing `almanaut-agent`, what it reports, conflicts and duplicates |
| [Background jobs](background-jobs.md) | The scheduled-tasks page, liveness checks, certificate probing, scheduled discovery |
| [Notifications & integrations](integrations.md) | ntfy & Discord expiry alerts, outbound webhooks, Uptime Kuma sync |
| [Export & import](export-import.md) | Whole-inventory YAML round-trip, additive CSV import |
| [JSON API](api.md) | Tokens and scopes, endpoints, the OpenAPI 3 spec |
| [Monitoring](monitoring.md) | Prometheus metrics, `/healthz`, `/version` |

Release notes live in [CHANGELOG.md](../CHANGELOG.md); repository conventions
for contributors are in [CLAUDE.md](../CLAUDE.md).
