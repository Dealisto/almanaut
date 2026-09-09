[← Documentation index](README.md) · [almanaut](../README.md)

# Background jobs

almanaut runs its recurring work — notifications, probes, scheduled discovery —
on one internal scheduler, with an admin page that shows what is registered and
lets you run any of it on demand.

Every job is **opt-in**: nothing below runs until its own configuration turns it
on, and a job that is off is not registered at all rather than registered and
idle. All the variables named here are described in
[Configuration](configuration.md).

## The scheduled-tasks page (`/tasks`)

**Admin-only**, like the other operational pages — an editor or viewer has no
nav entry for it and the route rejects them.

One row per registered job, showing its title, interval, when it last ran and
when it runs next, how long the last pass took, how many times it has run, and
the error from the last pass if there was one. A job currently executing is
marked as running.

Each row also has a **run now** button, which is the useful part when you are
setting something up: rather than editing an interval down to a minute to see
whether your ntfy topic works, trigger the job and read the outcome.

## What can be registered

| Job | Enabled by | Does |
|---|---|---|
| Expiry notifications | `ALMANAUT_NTFY_URL` and/or `ALMANAUT_DISCORD_WEBHOOK_URL` | Pushes alerts for certificates, warranties and renewals falling due — see [Notifications & integrations](integrations.md) |
| Liveness checks | `ALMANAUT_LIVENESS_ENABLED` | TCP-dials every host and service that has a check address |
| Certificate probing | `ALMANAUT_CERT_PROBE_ENABLED` | Reads expiry from the live TLS endpoint |
| Discovery: Docker | `ALMANAUT_DISCOVERY_DOCKER_INTERVAL` | Scheduled [Docker discovery](discovery.md#docker-containers) |
| Discovery: network | `ALMANAUT_DISCOVERY_NETWORK_INTERVAL` | Scheduled [network scan](discovery.md#network-scan) |
| Discovery: Proxmox | `ALMANAUT_DISCOVERY_PROXMOX_INTERVAL` | Scheduled [Proxmox discovery](discovery.md#proxmox-ve) |

## Liveness checks

With `ALMANAUT_LIVENESS_ENABLED=true`, a background pass TCP-dials each
monitored host and service and records up/down transitions.

Monitoring is **per entity, and opt-in by field**: set a **liveness check
address** (`host:port`, e.g. `192.168.1.10:443`) on the host's or service's edit
form. An entity with an empty check address is simply not monitored — enabling
the job does not start probing your whole inventory.

The host and service list pages gain a **Liveness** column reading `up` or
`down`, or `unknown` when an address is set but no result has come in yet. That
third state matters: `unknown` means "not checked", not "reachable".

Transitions are pushed through the same notification channels as expiry alerts,
so a host going down reaches the same ntfy topic or Discord webhook.

Note this is a TCP connect check, nothing more — a port that accepts a
connection counts as up even if the service behind it is broken.

## Certificate probing

Certificate expiry can be read from the live endpoint instead of typed in by
hand.

**The per-certificate "Probe now" button works whether or not the scheduled
job is enabled** — the prober is built regardless. `ALMANAUT_CERT_PROBE_ENABLED`
only controls whether probing also happens on a schedule, every
`ALMANAUT_CERT_PROBE_INTERVAL`. Each certificate's detail page shows the
outcome of its last probe, including when it has never been probed.

Probing on demand is a write action, so it needs an editor or admin role: a
viewer sees the last probe's result but cannot trigger a new one.

## Scheduled discovery & the run history (`/discovery/runs`)

Scheduled discovery is opt-in per source, each with its own interval variable.
The network scan additionally requires the scan itself enabled and a subnet set,
and Proxmox requires Proxmox configured; an interval alone is not enough.

**A scheduled run never imports anything.** Findings surface as proposals on the
[Discovery](discovery.md) page for you to review and import, exactly like a
manual scan — the schedule automates the looking, not the writing.

`/discovery/runs` is admin-only and keeps the **last 50 runs**, each with its
source, start and finish time, how many resources were found, how many of those
were new, and the error if the run failed. It answers the question the discovery
page cannot: whether the schedule is actually running, and whether it has been
failing quietly.

---

**See also:** [Configuration](configuration.md) · [Auto-discovery](discovery.md) · [Notifications & integrations](integrations.md) · [Monitoring](monitoring.md)
