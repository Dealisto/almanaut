[← Documentation index](README.md) · [almanaut](../README.md)

# The inventory model

Fifteen entity types, all sharing the same machinery — search, tags,
relationships, change history, journal, custom fields, attachments, the JSON
API, and CSV import:

| | |
|---|---|
| **Hosts** | Physical machines, VMs, LXC containers, and VPSes |
| **Services** | The things running on your hosts |
| **Networks** | Subnets, with built-in IPAM (usage, capacity, next-free IP) |
| **Domains** | DNS names / FQDNs |
| **Certificates** | TLS certs with expiry tracking |
| **Backups** | What's backed up, from where |
| **Hardware** | Devices with warranty tracking |
| **Subscriptions** | Recurring services with renewal dates |
| **Accounts** | Logins and secret references |
| **Sites / Locations / Racks** | Physical placement hierarchy (see below) |
| **Contacts** | People and vendors responsible for infrastructure |
| **VLANs** | 802.1Q VLANs referenced by networks |
| **IP reservations** | Named reserved ranges within a network |

## Sites, locations & racks

Physical placement is a **Site → Location → Rack** hierarchy: a site is a
building or campus, a location is a room or area within it, and a rack has a
height in rack units (U). Each level's detail page lists its children, so you
can navigate top-down.

Hosts and hardware can be **assigned to a rack and a U position** (with a
height in U) from their edit form. The rack's detail page then renders a
**U elevation** — a top-to-bottom diagram with each occupant drawn at its
position, linking to its detail page. Occupants that extend past the rack or
overlap another are highlighted; placement is advisory, not enforced at save
time.

## IPAM: VLANs & IP reservations

A network can reference a **VLAN** (name + 802.1Q ID) from its edit form; the
network's detail page shows the resolved `VLAN <id> (<name>)`.

**IP reservations** mark a named range within a network (a DHCP pool, a block
kept for switches). Reserved addresses show on the network's IPAM view, are
skipped by the "next free" suggestion, and are subtracted from the
free-address count.

## Custom fields

Admins define **custom fields** at `/custom-fields`: each field belongs to one
entity type and has a kind (text, number, bool, or date). Defined fields
appear on that type's edit forms and detail pages, their values are searchable
from global search, and they round-trip through the YAML export.

## Attachments

Any entity's detail page accepts **file attachments** (manuals, invoices,
config dumps) up to 16 MiB each, stored inside the SQLite database.
Attachments are **not** included in the YAML export — back up the database
file itself to keep them.

## History & journal

Every create, update, and delete is recorded automatically with a field-level
diff (e.g. `status: running → down`). Each entity's detail page shows its
**Journal** — manual, categorised notes (info / success / warning / incident)
you add as a running log — and a collapsible **Change history**. A global
**`/history`** feed (and a "Recent activity" panel on the dashboard) lists the
latest changes across every entity; delete events remain visible there even
after the entity is gone. Journal entries are included in the YAML export;
the change log is not.
