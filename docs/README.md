[← almanaut](../README.md)

# Documentation

| Page | What's in it |
|---|---|
| [Installation](installation.md) | Docker, Compose, prebuilt binaries, from source, first login, sample data |
| [Configuration](configuration.md) | Every environment variable, secrets from files, reverse proxy & TLS, the security model |
| [Authentication & access control](authentication.md) | Roles, sessions, login throttling, lockout recovery |
| [The inventory model](inventory-model.md) | The 15 entity types, sites/locations/racks, IPAM, custom fields, attachments, history |
| [Auto-discovery](discovery.md) | Docker containers, network scan, Proxmox VE |
| [Inventory agent](agent.md) | Installing `almanaut-agent`, what it reports, conflicts and duplicates |
| [Notifications & integrations](integrations.md) | ntfy & Discord expiry alerts, outbound webhooks, Uptime Kuma sync |
| [Export & import](export-import.md) | Whole-inventory YAML round-trip, additive CSV import |
| [JSON API](api.md) | Tokens and scopes, endpoints, the OpenAPI 3 spec |
| [Monitoring](monitoring.md) | Prometheus metrics, `/healthz`, `/version` |

Release notes live in [CHANGELOG.md](../CHANGELOG.md); repository conventions
for contributors are in [CLAUDE.md](../CLAUDE.md).
