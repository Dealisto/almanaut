[← Documentation index](README.md) · [almanaut](../README.md)

# Inventory health

Three read-only views answer three different questions about the inventory
itself, rather than about any one entity: what is wrong with it, what is about
to expire, and what breaks if one thing goes away.

## The health report (`/health-report`)

A fixed set of audit rules, each with its offender count and a drill-down list
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
| Duplicate IP assignments | The same IP address claimed by more than one host |
| Host IPs outside every network | Host addresses that fall in no known network |
| Overlapping networks | Networks occupying the same CIDR block (parent/child subnets excluded) |
| Stale entities | Entities untouched for longer than the staleness window (see below) |

The rules are fixed — there is no rule editor, and nothing here is configurable
beyond the staleness window. The dashboard's summary counter is produced by the
same builder as this page, so the two can never disagree about how many
findings exist.

The last three rules are the IPAM conflict checks. They are worth reading
alongside [IPAM](inventory-model.md#ipam-vlans--ip-reservations): "duplicate IP
assignments" and "host IPs outside every network" are the two ways an address
can be recorded wrongly, and "overlapping networks" deliberately tolerates a
parent/child subnet pair, because a `/24` containing a `/27` is normal
practice, not a mistake.

### Stale entities & acknowledging

An entity counts as stale when nothing has changed on it for longer than
`ALMANAUT_STALE_AFTER_DAYS` (default 90; set it to `0` to switch the rule off
entirely — see [Configuration](configuration.md)).

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
