# CLAUDE.md

Guidance for working in this repository.

almanaut is a single-binary Go homelab CMDB: SQLite storage, server-side
rendered UI (`html/template`), no client-side JS framework. Core layout:
`internal/{config,discovery,domain,store,web}`, entrypoint `main.go`; the
background jobs and integrations live alongside (`job`, `liveness`,
`certprobe`, `scheddiscovery`, `notify`, `webhook`, `kuma`), and the host agent
is `cmd/almanaut-agent` with `internal/{agent,agentapi}`.

## Build & test

- Build server: `go build -o almanaut .` (use `-o almanaut.exe` on Windows);
  both output names are gitignored. Build all packages: `go build ./...`.
- Build agent separately: `go build -o /tmp/almanaut-agent ./cmd/almanaut-agent`.
- CI (`.github/workflows/ci.yml`) checks formatting, `go vet`, `staticcheck`,
  `govulncheck`, tests with coverage, a `-race` pass, CGO-free builds, and a
  non-root container smoke test. CI is the authoritative test gate.
- Browser regressions: `node tests/browser/run.mjs` (a separate local check).
  Prerequisites, scope, and benchmarks are in [docs/testing.md](docs/testing.md).

## Conventions

### Entities are catalog-driven

Each entity is one `resource[T]` literal in `New` (`internal/web/server.go`)
plus a `*XRepo`. The generic CRUD handlers, the relationship catalog, and
global search all iterate that list — do not hand-write per-type handlers or
search blocks. Adding an entity means:

- the `resource[T]` literal, including its `search:` SQL expressions and
  `query:` metadata (table, label, ordering and filterable fields). Lists,
  global search, entity selectors and reference labels share that SQL metadata;
  keep it consistent with the resource's display labels and fields. The repo
  must satisfy `crud[T]`
  (`internal/web/resource.go`), `*Tx` methods included;
- its singular type in `domain.EntityTypes` (`internal/domain/relationship.go`),
  which gates relationships, tags, journal entries and custom fields;
- export/import in `internal/store/portability.go`: a `Snapshot` list, its
  `Export` line, its `validateAll` line, its insert in `replaceInventory`, its
  table in `inventoryTables`, and its `entityTables` entry.
  `TestEveryTableIsClassified` and `TestSnapshotHasOneListPerInventoryTable`
  fail until the table is covered;
- its count in the `almanaut_entities_total` list (`internal/web/metrics.go`).

### "Not found" vs real errors

Repository reads and `Update` return `store.ErrNotFound` (scan helpers map
`sql.ErrNoRows` via `notFound`; `Update` reports it via `rowsAffectedOrNotFound`
on zero rows affected). Web handlers branch with `notFoundOrServerError` (404
only for `ErrNotFound`, logged 500 otherwise) — never collapse every
`Get`/`Update` error to a 404.

### Entity detail URLs

Build entity paths with `entityCatalog.path(type, id)` (or a resource's
`basePath()`), never `"/" + type + "s"`. Route bases are irregular — hardware
lives at `/hardware`, not `/hardwares`.

### Transactions

Use `store.WithTx` (panic-safe); don't hand-roll `Begin`/`Commit`/`Rollback`.
Reads whose result drives a write must run on the tx-bound repo *inside* the
transaction — the discovery import handlers list existing entities inside
`WithTx`, and `Export` reads all tables in one read tx for a consistent
snapshot.

### Durable mutation events

Stage webhook events with `webhook.Stage` inside the same transaction as the
inventory and history writes, including changes to related records. Propagate
staging errors so a full outbox rolls back the mutation; call `Dispatch` only
after commit. Bulk writers reuse `webhook.ForTransaction(tx, dispatcher)` for
all staging in that transaction. Keep the returned dispatcher on one goroutine,
discard it when the transaction ends, and do not interleave other outbox or
endpoint writes or savepoint rollbacks that would invalidate its cached counts.

### Shared domain helpers (don't re-implement)

Date fields: `validateOptionalDate` / `validateRequiredDate` and the
`expiringOnOrBefore` filter (`internal/domain/dates.go`). IPAM's
network/broadcast reservation rule lives only in `reservesEnds`
(`internal/domain/ipam.go`) — used by both the capacity count and next-free.

## Gotchas / non-obvious constraints

### Do not cap the SQLite connection pool at one

`store.Open` (`internal/store/store.go`) deliberately leaves the
`database/sql` pool at its default (multiple connections). **Do not add
`db.SetMaxOpenConns(1)`.** The code reads from the database while a
transaction is open on the same `*sql.DB` — e.g. the import handlers
(`internal/web/discovery.go`) and the isolation check exercised by
`TestWithTxBoundRepoIsolatedUntilCommit`. With a one-connection pool the open
transaction holds the only connection while the concurrent read waits forever
for one — a deadlock (the test hangs to the 600s timeout). WAL mode plus
`busy_timeout(5000)` already serialize writers correctly, so no in-process cap
is needed. `TestOpenDoesNotCapConnectionPool` guards this.

### Write transactions are IMMEDIATE; read-only ones must opt out

`store.Open` sets `_txlock=immediate` in the DSN, so every read-write
transaction begins `BEGIN IMMEDIATE` and holds the write lock for its whole
body. This is deliberate: a transaction that reads and *then* writes cannot
otherwise be made reliable, because SQLite refuses the read-to-write lock
upgrade immediately — without invoking the busy handler — so `busy_timeout`
does not apply to it. `WithTx` therefore lets you read before you write.

The driver only applies the mode to transactions that are **not** declared
read-only. A transaction that genuinely only reads must begin with
`db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})`, or it will hold the write
lock and block every writer for as long as it runs — `Export`, which reads
every table for a consistent snapshot, is the reason this matters.
`TestReadOnlyTxDoesNotBlockWriters` guards it, and
`TestWithTxReadThenWriteSurvivesContention` guards the write path.

Note `ReadOnly: true` is only an opt-out of the locking mode here; the driver
does not enforce it, so a write inside such a transaction still executes — and
reintroduces exactly the failed upgrade the mode exists to prevent.

### IPAM attribution depends on *all* networks

In `internal/domain/ipam.go`, each host IP is attributed to the network that
contains it with the **longest prefix** (most specific), so computing one
network's occupancy correctly requires knowing every network — an IP that
belongs to a more-specific subnet must not be counted in a broader one.

- Use `BuildIPAM(networks, hosts, reservations)` for the full report (all networks).
- Use `BuildNetworkUsage(targetID, networks, hosts, reservations)` for a
  single-network view (the network detail page): it still attributes across all
  networks via the shared `attribute` helper, but calculates capacity and searches
  for a next-free address only for the target. The search is limited to subnets
  with at most 2^16 addresses and skips reserved intervals as ranges.

When changing attribution, keep both paths going through `attribute` so their
longest-prefix and tie-breaking rules stay identical.

Pass all reservations too: next-free suggestions exclude allocated IPs,
gateways, reserved ranges and reserved endpoints across overlapping networks,
even when local capacity attributes those addresses elsewhere.
