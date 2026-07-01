# CLAUDE.md

Guidance for working in this repository.

almanaut is a single-binary Go homelab CMDB: SQLite storage, server-side
rendered UI (`html/template`), no client-side JS framework. Layout:
`internal/{config,discovery,domain,store,web}`, entrypoint `main.go`.

## Build & test

- Build: `go build` (the resulting `almanaut.exe` is gitignored).
- Tests run in CI (GitHub Actions): `go vet ./...`, `go test ./...`, and a
  `-race` pass. CI is the authoritative test gate.

## Conventions

### Entities are catalog-driven

Each entity is one `resource[T]` literal in `New` (`internal/web/server.go`)
plus a `*XRepo`. The generic CRUD handlers, the relationship catalog, and
global search all iterate that list — do not hand-write per-type handlers or
search blocks. Adding an entity means filling in the literal (incl. its
`search:` field list, which feeds `/search`) and the repo; nothing else.

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

### IPAM attribution depends on *all* networks

In `internal/domain/ipam.go`, each host IP is attributed to the network that
contains it with the **longest prefix** (most specific), so computing one
network's occupancy correctly requires knowing every network — an IP that
belongs to a more-specific subnet must not be counted in a broader one.

- Use `BuildIPAM(networks, hosts)` for the full report (all networks).
- Use `BuildNetworkUsage(targetID, networks, hosts)` for a single-network view
  (the network detail page): it still attributes across all networks via the
  shared `attribute` helper, but runs the expensive per-network capacity /
  next-free enumeration (up to 2^16 addresses) only for the target.

When changing attribution, keep both paths going through `attribute` so their
longest-prefix and tie-breaking rules stay identical.
