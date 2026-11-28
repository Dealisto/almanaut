# SNMP and LLDP observations

Almanaut can collect a bounded inventory observation from explicitly configured
SNMP devices. Collection uses only SNMP GET and GETBULK requests. It never sends
SET requests, provisions a device, scans an address range, or automatically
creates cable connections. The implementation uses [GoSNMP](https://github.com/gosnmp/gosnmp)
and its [documented API](https://pkg.go.dev/github.com/gosnmp/gosnmp).

## Configure sources

Set `ALMANAUT_SNMP_SOURCES_FILE` to a JSON file containing an array. No file means
no SNMP sources. Restart the server after changing this configuration. A nonsecret fingerprint of the
configured endpoint, protocol, user/context, and explicit inventory owner binds
observations and review tokens. Changing that configuration hides old observations
and disables their actions until a successful scan. Credential rotation alone does
not change the configured device identity.

```json
[
  {
    "id": "core-switch",
    "name": "Core switch",
    "target": "192.0.2.10",
    "port": 161,
    "version": "3",
    "username": "inventory-reader",
    "auth_password_file": "secrets/core-auth",
    "privacy_password_file": "secrets/core-privacy",
    "interval": "15m",
    "owner_type": "hardware",
    "owner_id": 42
  }
]
```

The example address is reserved for documentation. Replace it and the owner ID
with the intended device and an existing inventory record. `owner_type` accepts
`hardware` or `host`; omit both owner fields for observation only. The system
name, serial number, address, and LLDP identifiers never establish this mapping.
An inventory replacement can change the meaning of numeric IDs; review source
owner configuration after restoring or replacing inventory.

SNMPv3 is the default and requires **authPriv, SHA-256 authentication, and AES-128
privacy**. Both passphrases must contain 8–1024 bytes; the username is required.
An optional `context_name` selects a device context. SNMPv1, unauthenticated v3,
and older authentication/privacy algorithms are not supported.

Credentials are read from files, relative to the sources file unless absolute.
A trailing newline is accepted. Keep these files readable only by the server's
operator account. Do not place plaintext secrets in the JSON configuration;
unknown fields are rejected. Secrets are not serialized into observations,
inventory export, or error messages.

For devices that require it, explicitly select `"version": "2c"` and provide
`"community_file": "secrets/legacy-community"`, omitting all v3 fields.
SNMPv2c transmits the community and observations without encryption. Configure a
read-only device account/view and restrict device access to the Almanaut server.

`interval` is optional: omission or `"0s"` disables automatic scans. Positive
intervals must be at least one minute. Scheduled scans appear in background
jobs. An administrator can also use **Discover → SNMP & LLDP → Scan now**.
Viewing observations does not trigger network requests. The sources file is
limited to one MiB and 32 sources with unique IDs.

## Collected data and limits

The collector reads selected standard columns, not entire device MIB trees:

- SNMPv2-MIB system name, description, and object identifier.
- IF-MIB interface index, name/description, alias, type, MAC, speed, and
  administrative/operational state.
- ENTITY-MIB chassis manufacturer, model, and serial, when exactly one chassis
  is reported. Multiple chassis remain explicitly ambiguous.
- LLDP-MIB local port identifiers and remote chassis/port identifiers,
  descriptions, and advertised system name.

A scan has a 30-second overall deadline, two-second request timeouts, one retry,
25 repetitions per bulk request, 512 interface/local-port/chassis rows per table,
512 neighbor rows, and a combined 20,000-variable limit. A failed required
interface read does not replace the previous successful snapshot. Optional
unsupported tables produce warnings. Incomplete LLDP records cannot be used to
confirm a cable. Some devices do not expose LLDP or ENTITY-MIB; their absence is
not evidence that a device, interface, or neighbor disappeared.

Only the latest successful observation and last attempted scan status are
persisted per source. Observations are operational state: full database backups
include them; portable YAML inventory exports exclude them, and inventory
replacement clears them. Raw upstream errors are not persisted, since a library
or device error may contain credentials. A failed scan leaves previous data
visible with its original observation timestamp. A started scan that times out or
is canceled persists a sanitized failed-attempt status using a separate two-second
write deadline; cancellation while queued does not count as a device scan.

## Review inventory changes

An editor or administrator can create selected **new physical ports** on the
explicitly configured owner. The UI only offers uniquely named Ethernet-type
interfaces as candidates. An Ethernet interface can still be virtual; the
operator must confirm that it represents a physical connector. Existing names
are rejected, not updated. Each import re-scans the source, checks that selected
observations and the owner are unchanged, and commits the entire selection with
audit history and durable webhook staging. A failure rolls back the batch.

The initial integration intentionally does not turn ifIndex values into stable
inventory bindings: device indexes can change. It does not update manual port
names, notes, interface/IP records, MAC addresses, or device identities. Logical
interfaces and IP assignments can be maintained through the structured network
inventory pages separately.

For LLDP, select **Review connection**, choose both known inventory ports, and
review their displayed owners/names before confirming. The local selector is
restricted to the configured owner and the remote selector is searchable.
Neither selector guesses a match from LLDP names. The local selector includes
up to 512 free ports; the remote selector shows up to 100 free matches.

Confirmation performs a fresh scan and then rechecks both port records and owner
names inside the write transaction. Changed observations, changed inventory,
missing owners, or already connected ports are rejected. Both reciprocal cable
changes are audited and stage webhooks atomically. Unrelated notes and custom
fields are retained. LLDP time marks are excluded from comparison, while actual
identity fields remain part of it. An LLDP local port number is **not** assumed
to equal an IF-MIB interface index.

The resulting physical topology records the operator-confirmed cable. It does
not prove layer-2/layer-3 reachability, current link health, or outage impact.

## Validation and device prerequisites

Automated tests cover fixture MIB observations, collection limits, incomplete
LLDP, SNMPv3 client security options, credential redaction, snapshot retention,
permissions, stale reviews, atomic rollback, and reciprocal audit/events.
Loopback UDP tests exercise actual GET/GETBULK encoding/decoding and cancellation;
they are protocol fixtures, not live-hardware compatibility certification.

A real-device rollout still needs the intended target address/port, compatible
SNMPv3 credentials and context, allowed source network, supported MIB views,
LLDP enabled on relevant links, and a verified owner/port mapping. No vendor,
firmware, management address, credential, port numbering scheme, or real cable
identity can be inferred from the repository alone.
