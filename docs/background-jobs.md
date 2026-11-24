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
| Discovery: Docker / Podman | `ALMANAUT_DISCOVERY_DOCKER_INTERVAL` | Scheduled [container discovery](discovery.md#docker-containers) |
| Discovery: network | `ALMANAUT_DISCOVERY_NETWORK_INTERVAL` | Scheduled [network scan](discovery.md#network-scan) |
| Discovery: Proxmox | `ALMANAUT_DISCOVERY_PROXMOX_INTERVAL` | Scheduled [Proxmox discovery](discovery.md#proxmox-ve) |
| Discovery: Xen Orchestra | `ALMANAUT_DISCOVERY_XO_INTERVAL` | Scheduled [XCP-ng discovery](discovery.md#xcp-ng-via-xen-orchestra) |
| Discovery: configured source name | Positive `interval` in the discovery source file | One independent job per configured Docker, Podman, Proxmox or XO source |

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

Scheduled discovery is opt-in per source. Without a source file, the interval
variables above control the default integrations. With
`ALMANAUT_DISCOVERY_SOURCES_FILE`, each entry's positive `interval` enables its
own job, labelled with the source's display name. The file replaces the legacy
engine intervals as well as their connection settings. The network scan remains
separate and additionally requires the scan enabled and a subnet set.

Engine run history and notification comparisons are isolated by the stable
`type:id` source key, such as `proxmox:lab` or `docker:default`. Two sources can
report the same external identifier without sharing their comparison baseline.
Older history rows retain their original source labels; new engine runs use the
namespaced keys. Resource matching uses the same persistent identities and
manual-record matching rules as the import preview.

**A scheduled run never imports anything.** Findings surface as proposals on the
[Discovery](discovery.md) page for you to review and import, exactly like a
manual scan — the schedule automates the looking, not the writing. It also
never confirms a manual record's source identity; that association is saved only
by an explicit import. Renaming a bound resource does not make it a new finding.

A successful engine scan also records when each unambiguously associated
resource was last observed. Failed scans and resources missing from a successful
scan leave their previous observation times intact. These are operational
timestamps, not edits to inventory fields, and they are excluded from YAML
exports. Source review pages also record successful observations independently
of the schedule.

`/discovery/runs` is admin-only and displays the **last 50 runs**, each with its
source, start and finish time, how many resources were found, how many of those
were new, and the error if the run failed. **Discover** shows a compact summary
on each source card: last attempt, last success, the resource count at that
success, and the estimated next scheduled scan. A later failure retains the
previous successful count. Admins can start the existing job from that card or
open its full history; scheduler controls remain admin-only. Reviewing a source
fetches current data without creating a background-run history entry.

Recording a run also prunes stored history in the same transaction. The database
retains the last 50 runs plus each source's latest run and most recent successful
run when those fall outside the displayed window. Keeping these small per-source
baselines preserves the last known status and prevents a long outage from making
previously discovered resources appear new again.

---

**See also:** [Configuration](configuration.md) · [Auto-discovery](discovery.md) · [Notifications & integrations](integrations.md) · [Monitoring](monitoring.md)
