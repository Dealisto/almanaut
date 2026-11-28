[← Documentation index](README.md) · [almanaut](../README.md)

# Inventory health

Three read-only views answer three different questions about the inventory
itself, rather than about any one entity: what is wrong with it, what is about
to expire, and what breaks if one thing goes away.

## The health report (`/health-report`)

A configurable selection of built-in audit rules, each with its offender count and a drill-down list
of the entities that tripped it:

| Rule | Finds |
|---|---|
| Hosts without a backup | Hosts not linked to any backup entity |
| Services not linked to a host | Services floating without the host they run on |
| Expired certificates | Certificates whose expiry date has already passed |
| Certificates linked to nothing | Certificates not attached to any entity |
| Hardware without a warranty date | Hardware whose warranty end is unknown |
| Subscriptions without a renewal date | Subscriptions whose renewal date is unknown |
| Orphaned entities | Entities with no relationships at all |
| Duplicate IP assignments | The same IP address claimed by multiple inventory owners |
| IP allocations outside every network | Allocated addresses that fall in no known network |
| Overlapping networks | Networks occupying the same CIDR block (parent/child subnets excluded) |
| Stale entities | Entities untouched for longer than the staleness window (see below) |

Administrators can open **Settings → Health rules** (`/health-rules`) to enable
or disable individual checks. The dashboard summary and health report use the
same settings and evaluator. Disabled checks disappear from the report and
contribute no findings. These settings do not change inventory, scheduled
probes, notification windows or the separate `/checks` page.

An optional **Certificates expiring soon** check is disabled by default. Its
warning window defaults to 30 days and accepts 1–3650 days. It includes dates
through the last day of the window, excluding certificates already expired
(including today's expiry), which remain covered by the expired-certificates
check. **Backup job health** reports configured backup jobs that have never
reported, failed, or exceeded their expected interval; each backup controls its
own interval, independently of inventory staleness.

A blank staleness override inherits `ALMANAUT_STALE_AFTER_DAYS`. A value of 0
disables stale detection; 1–36500 sets a local override. The Stale entities
check must also be enabled. **Restore default health rules** restores the
built-in selection, the 30-day certificate warning window and server staleness
inheritance. With no saved settings, existing report behavior is preserved.

Settings writes and resets require an administrator and CSRF protection. They
are recorded in the administrative audit log with before/after values. Settings
persist across restarts and inventory replacement; they are excluded from
JSON/YAML inventory exports and included in complete database backups. Rules
are built in; administrators cannot submit SQL, scripts or expressions.

The duplicate-IP, unassigned-IP, and overlapping-network rules are the IPAM
conflict checks. They are worth reading
alongside [IPAM](inventory-model.md#ipam-vlans--ip-reservations): "duplicate IP
assignments" and "IP allocations outside every network" are the two ways an address
can be recorded wrongly, and "overlapping networks" deliberately tolerates a
parent/child subnet pair, because a `/24` containing a `/27` is normal
practice, not a mistake.

### Stale entities & acknowledging

An entity counts as stale when nothing has changed on it for longer than
`ALMANAUT_STALE_AFTER_DAYS` (default 90; set it to `0` to switch the rule off
entirely — see [Configuration](configuration.md)). Administrators can override
this window or disable the check in Health rules.

Staleness is about *your* attention, not the machine's, so the stale rule is the
only one with an **acknowledge** button next to each finding. Acknowledging
resets that entity's staleness clock and records the acknowledgement in the
entity's [change history](inventory-model.md#history--journal) — so "yes, I
looked at this and it is still correct" is itself a durable, attributable fact
rather than a silent dismissal.

Acknowledging is a write action and needs an editor or admin role. The report
itself — like `/checks` and `/impact` — is readable by everyone, viewers
included.

## Checks (`/checks`)

A narrower, time-based view: everything falling due within the **next 30 days**,
in four lists.

- Services with no backup relationship
- Certificates expiring
- Hardware warranties expiring
- Subscription renewals due

The 30-day window here is fixed and is not
`ALMANAUT_NOTIFY_WITHIN_DAYS` — that variable governs what
[expiry notifications](integrations.md#expiry-notifications-ntfy--discord) push
to ntfy or Discord. Widening the notification window does not widen this page.

## Impact analysis (`/impact`)

Pick any entity and get every entity that **transitively depends on it** — the
blast radius of taking it down.

The traversal follows only dependency-kind relationships, in one direction: the
dependent is the relationship's *from* end and the dependency is its *to* end.
So asking about a host returns the services running on it, plus whatever depends
on those services in turn, and so on outward. It is breadth-first and cycle-safe,
and the entity you asked about is not itself included in the answer.

This is the inverse of the neighbourhood graph on an entity's detail page: the
graph shows one hop in every direction, while impact follows one direction for
as many hops as exist.

---

**See also:** [The inventory model](inventory-model.md) · [Lists, saved views & bulk editing](lists-and-views.md) · [Background jobs](background-jobs.md) · [Configuration](configuration.md)
