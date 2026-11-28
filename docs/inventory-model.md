[← Documentation index](README.md) · [almanaut](../README.md)

# The inventory model

Seventeen entity types, all sharing the same machinery — search, tags,
relationships, change history, journal, custom fields, attachments, the
[JSON API](api.md), and [CSV import](export-import.md#additive-csv-import).
Every list page can also be [filtered, sorted, saved as a view, and edited in
bulk](lists-and-views.md), and the inventory as a whole is audited on the
[health report](inventory-health.md):

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
| **NICs** | Network cards on a host (onboard or expansion) |
| **Ports** | Physical network ports on hosts and hardware, and the cables between them |

## Host types

Open **Settings & data → Host types** (`/host-types`) to add a custom type,
rename its display label, or delete it. Editors and administrators can manage
the catalog. Types appear in host forms and network discovery imports.

Each type has a permanent name used in the API and imports, and a display
label shown on host lists and details. Renaming the label preserves every
host's type assignment. The built-in names `physical`, `vm`, `lxc`, and `vps`
can be relabeled but cannot be deleted: Proxmox discovery and agent reports
use these stable names. Agent updates preserve a manually selected custom type.

An unused custom type can be deleted directly. For a type still assigned to
hosts, select a replacement before deleting it. All affected hosts are updated
in one transaction; their other details and custom fields are preserved, and
the changes appear in host history and update webhooks.

If a type disappears while a host form is open, a failed save preserves the
submitted type as unavailable. Select a valid replacement explicitly before
retrying; the form does not silently select the first catalog entry. An empty
submitted type likewise remains empty until a choice is made.

## Navigating a record

Record pages have three sections:

- **Overview** contains the fields, tags, notes, custom fields, and any agent or
  certificate probe information.
- **Connections** contains relationships and diagrams, child records, port
  connections, IP allocations, and rack elevations when applicable.
- **Activity & files** contains attachments, journal entries, and change history.

Section links can be bookmarked. With JavaScript enabled, use the arrow keys to
move between tabs; without it, all three sections remain visible on one page.
Journal and attachment actions return to **Activity & files**.

Relationship lists and their neighborhood graphs resolve names only for
connected records, using one read-only snapshot for the relationships and their
labels. Other detail sections are loaded independently. Deleted neighbors remain
identified as deleted, without a link to a missing record.

### Form validation

If an integer field fails validation, forms retain the submitted text rather
than replacing it with zero. This covers rack positions and device heights,
rack height, and VLAN IDs. Retrying the same invalid draft does not change the
stored value; correct the field explicitly before saving.

With JavaScript enabled, **System / Light / Dark** changes appearance without
reloading the page or losing unsaved form inputs. The preference is saved in the
browser's cookie. If saving fails, the current page stays open with a status
message; retrying does not submit the inventory form. Without JavaScript, the
theme form keeps its normal submit-and-return behavior.

## Relationships

Entity selectors load IDs and labels in catalog order, including composed port
and NIC names, without loading full inventory records and their decorations.
They still include the complete inventory, so the option count grows with it.

The **Relationships** page links a source entity to a destination with a chosen
relationship kind. If a submission is invalid, the form keeps all three choices
for correction. An entity deleted in another tab remains visible as an
unavailable choice instead of being silently replaced by the first option.
Choose an existing endpoint before retrying; invalid submissions create no link.

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
time. When placements conflict, a separate list keeps every occupant's position
and detail link available, including records obscured by another in the diagram.

## NICs & ports

A **port** is a physical network port on a host (a NIC port) or on a hardware
item (a switch or router port). Set a port's **Connected to** field to record
the cable at its other end. The link is stored on both ports, so it shows on
both ports' detail pages and on both owners', and either end can change or
clear it. A port has one cable: the form only offers free ports, and a port
that is already connected elsewhere is refused (by the API and CSV import
too).
If a submitted NIC or peer port is no longer available when the form is
redisplayed, its selection remains visible as unavailable. Retrying does not
silently replace it with **none** or **not connected**. Choose a valid target
or explicitly clear the relation before saving.

A host's port can also belong to a **NIC**, an onboard or expansion card of
that same host. Deleting a NIC, or moving it to another host, keeps the ports
and only clears their NIC. The new-port page can generate "Port 1" to
"Port N" for an owner in one step, skipping names that already exist, which
is the quick way to fill in a 24-port switch.
If the generator rejects the input, it keeps the submitted owner, prefix and
count for correction, together with the original list filters. The single-port
form on the same page remains independent. Duplicate-name checks read only
the selected owner's names inside the generation transaction.

## Physical topology

Open **Topology** (`/topology`) to explore recorded cables between hosts and
hardware. Select a site or rack, show devices without a resolved site, or follow
a device's **Connections** link in the topology table to focus on that device.
Each selection includes directly connected peers, marked **Outside selection**
when they are not among the selected devices. Following another device's link
continues the exploration one hop at a time. Scope is kept in the URL.

Site membership follows the device's rack, its location, and its site. Devices
without that complete hierarchy are shown by **Without a site**; a hardware
record's free-text location is not treated as a site assignment.

The diagram and accessible cable table show both port endpoints, with links to
their inventory records. Reciprocal port records represent one cable; parallel
cables and connections between two ports on the same device remain distinct.
Missing device records are labelled without a dead link. Missing or nonreciprocal
port links are omitted with a warning. **Download SVG** exports the current
scope as a standalone diagram.

Views contain at most 60 selected devices, 100 devices including their peers,
and 200 cables. A notice identifies a partial view and asks you to narrow the
scope. Site and rack selectors contain at most 100 entries, always retaining
the selected existing record. Device labels and port names are limited to 160
characters in this view; diagram labels are shortened further, with the full
display label in each node's title and the table. These bounds limit loaded data
and output, rather than promising a fixed database query cost.

Only recorded physical port connections are drawn. The topology does not infer
VLANs, routing, reachability, redundancy, or outage impact from adjacency. The
separate [impact view](inventory-health.md#impact-analysis-impact) continues to
use declared dependency relationships.

## IPAM: VLANs & IP reservations

A network can reference a **VLAN** (name + 802.1Q ID) from its edit form; the
network's detail page shows the resolved `VLAN <id> (<name>)`.

**IP reservations** mark a named range within a network (a DHCP pool, a block
kept for switches). Reserved addresses show on the network's IPAM view, are
skipped by the "next free" suggestion, and are subtracted from the
free-address count.

The gateway also consumes one usable address unless a host or reservation
already occupies it. Overlapping reservations count once. IPv4 network and
broadcast addresses are excluded from usable capacity, except on /31 and /32
networks; they are not subtracted again when a reservation includes them.

When networks overlap, host usage is attributed to the most specific subnet;
equal prefixes use the first network in inventory order. Capacity counts follow
that attribution. The "next free" suggestion additionally excludes every IP
assigned to a host, including hosts attributed to a child subnet or an equivalent
network. It also excludes gateways, reserved ranges, and IPv4 network/broadcast
addresses belonging to overlapping networks; /31 and /32 networks have no
reserved endpoints. IPv4 and IPv4-mapped IPv6 spellings identify the same address.
Native IPv6 reservations do not reserve IPv4 addresses. Reservation ranges are
clipped to their own networks before they affect suggestions elsewhere.
A network can therefore have a positive local free-address count but no safe
suggestion when overlapping networks occupy or reserve all candidates.

Reservation counts remain exact for networks with up to 30 host bits, including
networks too large for the next-free search. That search is limited to 65,536
addresses; larger networks leave the suggestion empty. Networks with 31 or more
host bits show unbounded capacity instead of a numeric free-address count.
Within that search bound, reserved intervals are skipped as whole ranges instead
of visiting each address. Local capacity and global exclusions reuse parsed
reservation ranges, reducing work for inventories with many reservations.

## Tags

Tags are free-form labels on any entity. Each name is normalized before it is
stored — surrounding whitespace trimmed, a leading `#` dropped, and the rest
lowercased — so `#Prod`, `prod` and ` PROD ` are all the same tag rather than
three near-duplicates. Only one leading `#` is removed: enter `##prod` to
store the distinct literal name `#prod`. Tag overview links and YAML exports
preserve that distinction.

**Tags** (`/tags`) is their cross-type view: a cloud of every tag in use with
how many entities carry it, and clicking one drills down to that tag's
entities — across every type at once. That is what tags are for that
relationships are not: a `needs-upgrade` or `prod` label can span hosts,
services and certificates, and this page is the only place that collects them
into one list.

A tag's entity list resolves only its referenced records, using the same
snapshot for associations and names. It preserves composite port and NIC names
without loading the rest of the inventory.

Tags are searchable from global search, filterable on
[list pages](lists-and-views.md#filtering--sorting), applicable in
[bulk](lists-and-views.md#bulk-actions), and round-trip through the YAML export.

## Custom fields

Admins define **custom fields** at `/custom-fields`: each field belongs to one
entity type and has a kind (text, number, bool, or date). Defined fields
appear on that type's edit forms and detail pages, their values are searchable
from global search, and they round-trip through the YAML export.
Numeric values such as `+12`, `1_000` or `0x1p2` remain editable as text when a
native number input would erase them. Dates use the same fallback for values
such as year zero or surrounding spaces; this also applies to inventory date
fields. Invalid drafts remain visible after an error. Ordinary compatible
values retain native number controls and date pickers, and validation remains
on the server.
If creating a definition fails validation or conflicts with an existing name,
the form retains its label, optional name, entity type, and value type. A failed
label edit keeps the affected editor open with the submitted text.

Boolean values accept `true`, `on`, `1`, `yes`, or `y`, and `false`, `off`,
`0`, `no`, or `n` (case-insensitive, with surrounding whitespace ignored).
An empty checkbox value means false. Other values are rejected, including
during YAML import, so a typo cannot silently replace a stored true value.

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
Activity links resolve only the referenced record IDs; the frozen event labels
remain visible even after a rename or deletion. Delete and import events stay
unlinked.

---

**See also:** [Lists, saved views & bulk editing](lists-and-views.md) · [Inventory health](inventory-health.md) · [JSON API](api.md) · [Export & import](export-import.md)

### Structured interfaces and IP addresses

Network → Interfaces models physical, virtual, bridge, bond, VLAN and loopback
interfaces independently from physical NIC cards and cable ports. Each interface
belongs to a host or hardware item; its optional physical port must belong to the
same owner. The VLAN, MAC, MTU and administrative enabled flag are documented
inventory, not live reachability measurements. Interface names are unique within
an owner. Unassign addresses before deleting their interface.

Network → IP addresses stores IPv4/IPv6 allocations with optional interface
assignment, DNS name and active/reserved/deprecated status. Bare IPs become /32
or /128; a supplied prefix preserves host bits. All statuses occupy the address
until the record is removed. Unassigned allocations still block next-free
suggestions. VLAN association does not create a separate address space: IPAM
remains global, with longest-prefix attribution across all configured networks.

Legacy host IP lists and agent reports continue unchanged. Explicit allocations
are independently maintained; agents do not modify interfaces or address records.
IPAM combines both sources in one read snapshot and counts repeated addresses on
one actual host or hardware owner once. Different owners claiming an address
are conflicts. Separate unassigned allocations remain separate claims. Interface
and IP address details, API CRUD, tags, custom fields, history, CSV and inventory
exports use the same entity catalog as existing inventory.

Missing references in imported snapshots remain visible for repair. An existing
port and interface cannot disagree about ownership, including during imports or
port moves. An edit does not silently clear a missing selected reference.
