# Testing

Run the normal Go checks from the repository root:

```sh
go vet ./...
go test ./...
go test -race ./...
```

The repository's CI also checks `gofmt`, runs `staticcheck` and `govulncheck`,
collects test coverage, builds all Go packages with CGO disabled, and builds and
smoke-tests the non-root container. See
[the CI workflow](../.github/workflows/ci.yml) for the exact commands.
Browser regressions are currently a separate local check, not a CI job.

## Before publishing a release

Validate the exact revision intended for release, including the CI checks above,
and review the [release notes](../CHANGELOG.md), installation instructions,
configuration defaults, and [backup and rollback compatibility](export-import.md#export-format-compatibility).
Use a copy of existing data to verify upgrades and restoration with the matching
old binary and database backup.

To validate the distribution configuration and generate local archives without
publishing, run GoReleaser v2 from a checkout containing the Git tags:

```sh
goreleaser check
goreleaser release --snapshot --skip=publish,announce
```

The snapshot version is derived from Git metadata; it does not select the next
release number. Verify the eight server/agent archives in `dist/`, their
`checksums.txt`, and the agent archive's installer, systemd units and `agent.md`.
Test container builds for both `linux/amd64` and `linux/arm64`, including a fresh
data volume, the configured healthcheck and restart persistence.

Pushing a `v*` tag immediately starts the separate
[binary release](../.github/workflows/release.yml) and
[container publication](../.github/workflows/publish.yml) workflows. These
workflows do not wait for the CI workflow, so confirm CI has passed on the exact
commit before pushing a release tag. A local snapshot does not publish anything.

## Browser regressions

The browser runner uses Go, Chromium, and Node.js 22.4 or newer. It uses only
Node's built-in modules, so there is no package installation step or npm lockfile.

```sh
node tests/browser/run.mjs
```

Set `CHROMIUM` to a different Chromium-compatible executable when needed:

```sh
CHROMIUM=/usr/bin/chromium node tests/browser/run.mjs
```

Chromium's sandbox remains enabled by default. In a container that requires
disabling it, explicitly set `CHROMIUM_NO_SANDBOX=1` for this command.

The runner builds a small Go test server, creates a temporary SQLite inventory,
and starts Chromium headlessly with a fresh temporary browser profile. Both HTTP
servers and the debugging endpoint listen on loopback only. The test helper
disables login for this disposable inventory; it does not connect to an existing
Almanaut instance or load the application's environment configuration.

Checks exercise the production JavaScript and CSS, plus real server-rendered
inventory and search pages:

- Host agent-protection checkboxes survive validation, save through the normal
  mutation path, and explicitly clear protection when all are unchecked.
- Physical topology restores scope, navigates between devices, exports valid
  standalone SVG, and keeps its diagram inside a scrolling panel on mobile.
- Detail-tab selection, keyboard navigation, modified clicks, browser history,
  and preservation of modified keyboard shortcuts such as Alt+Left.
- Revealing a hidden panel and scrolling to a deep-link heading.
- Bulk-action state when `pageshow` follows restored checkbox values, including
  partial selection and clearing all rows. The restoration event is simulated;
  this does not assert a browser's cache admission policy.
- Discovery selection controls: select/clear all eligible imports or editable
  fields within one form, keep unrelated host-link preferences, show mixed
  selections and counts, prevent empty actions, and reconcile restored values.
  A real server-rendered source page is checked against a deterministic fake
  container scanner; no external engine is contacted.
- Human-readable selection labels for all 17 inventory types, including quotes,
  ampersands, and HTML-looking names where domain validation permits them.
- Distinct empty-search and out-of-range-search messages, with a working
  first-page URL that preserves the query.
- List-to-detail return links for all 17 entity types, preserving filter, sort
  and page state, plus actual navigation through a detail tab back to a filtered
  list. Go tests reject external, malformed and unrelated return destinations.
- New and Edit form navigation for all 17 inventory types, including actual
  Cancel navigation and a validation-error/corrected-save flow that preserves
  both the entered draft and the original list controls.
- Adding and deleting tags, journal entries and attachments from a detail page,
  preserving the list return context and Activity tab through real submissions.
- Custom-field creation and label-edit retries, preserving draft values and
  selected types after validation errors without interpreting entered HTML.
- Relationship retries after self-links or deleted endpoints, preserving the
  source and kind while correcting only the destination. Empty selections use
  native required-field validation; unavailable endpoints stay explicit.
- Bulk port generation retries with HTTP 400, retaining the exact draft and
  list controls; correcting only the count creates ports on the intended owner.
- Single-port edits with a peer taken by another edit or a NIC deleted after
  loading: failed retries preserve both the unavailable choice and stored links
  until the user explicitly selects a valid target or clears the relation.
- Host edits after a custom type is deleted: failed retries preserve the
  unavailable type and the stored replacement until a valid type is selected.
- Numeric custom fields and dates accepted by the API but incompatible with
  native inputs: editing unrelated fields preserves their values, while invalid
  drafts remain visible for correction after a validation error.
- Integer fields after server rejection, including exponent notation accepted
  by native number inputs: retries retain the draft and leave stored values and
  history unchanged until correction.
- Out-of-range states for all 17 inventory lists, with a first-page link that
  preserves the query, sorting, and page size; empty filters stay distinct.
- Mobile navigation focus, keyboard trapping, Escape, and search shortcuts on
  the real hosts page, including switching from desktop to a narrow viewport.
- Mobile navigation accessibility, scroll locking, and focus recovery when
  `pageshow` follows restored toggle values. As with selection, restoration is
  simulated rather than asserting browser cache admission.
- Print-media styles with the mobile drawer open, so its overlay and scroll
  lock cannot obscure the printed inventory.
- A consistent light print palette with either explicit or system dark mode.
- Appearance changes preserve the current document and unsaved inputs, with
  accessible selection/status updates, keyboard use and safe error handling.
- Native validation on the host-type form reveals invalid advanced fields in
  collapsed disclosures, focuses the offending control, and permits submission
  after correction without unnecessarily reopening the disclosure.
- Accessible rack occupant links when equipment overlaps or is positioned
  outside rack capacity, checked with browser hit testing.

Every assertion is printed as `PASS` or `FAIL`; a failed assertion, server error,
browser startup failure, or timeout exits nonzero. The runner stops its processes
and deletes the temporary database/profile on completion, failure, or interruption.
The complete run has a 120-second deadline. No personal browser profile is used.

These focused checks complement the Go suite. They do not replace visual review,
screen-reader testing, or cross-browser compatibility testing.

## Authentication during request ingestion

Regression tests change credentials or roles after initial authentication but
before the body reader finishes, without timing-dependent sleeps:

```sh
go test -race ./internal/web -run 'TestHTMLMutationRechecksAuthenticationAfterBodyRead|TestAPIMutationsRecheckBearerAfterBodyRead'
```

The API cases cover entity creation, replacement and agent reports with revoked
or expired tokens, reduced roles or scopes, storage failures, unchanged access
and renamed owners. They assert that rejected requests leave inventory and
history unchanged and that revalidation does not duplicate token-use audit events.

## Numeric sorting benchmark

Measure comparisons of long decimal values that round to the same floating-point
number, including underflow to zero:

```sh
go test ./internal/store -run '^$' -bench 'BenchmarkInventory(LongDecimal|UnderflowDecimal)Order' -benchmem
```

The nonzero case uses million-digit inputs; the underflow case uses 200,000
fractional zeros followed by differing digits and an `e-1000` exponent. These
measure the comparator, not complete list queries or rendering. Regression tests
also cover exact ordering, negative values, equivalent decimal notation and
compatibility with the existing extreme-exponent and hexadecimal fallback.

## IPAM benchmarks

Run the attribution and overlap benchmarks with allocation reporting:

```sh
go test ./internal/domain -run '^$' -bench 'BenchmarkIPAMAttribution|BenchmarkIPAMOverlapsDistinctNetworks' -benchmem
```

The attribution benchmark includes both the indexed implementation and the
original linear reference, using the same mixed IPv4/IPv6 inventory. Index
construction is included in the measurement. Differential tests compare their
allocations, including mapped IPv4 addresses, duplicate prefixes and invalid
inputs. Timings depend on inventory size and the machine running the benchmark.

Measure fully reserved subnets and large reservation inventories separately:

```sh
go test ./internal/domain -run '^$' -bench 'BenchmarkBuildIPAMReservedSubnet|BenchmarkBuildIPAMManyReservations' -benchmem
```

Next-free differential tests compare range skipping with independent bounded
enumeration across IPv4, IPv6, mapped addresses and maximum-address boundaries.

## Webhook admission benchmark

Measure one event staged against an existing queue with one or more matching
endpoints:

```sh
go test ./internal/webhook -run '^$' -bench '^BenchmarkDurableStageBacklog$' -benchmem
```

Each sample rolls back so the backlog stays constant. Capacity accounting is
shared within a single staging call. CSV imports, bulk actions, port generation,
host-type replacements and discovery imports additionally use an explicit
transaction scope across rows.
Regression tests cover concurrent database
writers, exact byte limits, endpoint invalidation between staging calls, and
rollback of both inventory and deliveries after failed admission.

Measure the per-row staging pattern with a single endpoint and a shared
transaction, comparing independent calls with the CSV transaction scope:

```sh
go test ./internal/webhook -run '^$' -bench '^BenchmarkTransactionStagingRows$' -benchmem
```

This measures webhook staging, excluding CSV parsing and inventory/history
writes. CSV integration tests additionally cover indirect port events,
cancellation, capacity rollback, and post-commit legacy dispatch.
Discovery integration tests cover rollback of bindings and relationships,
duplicate imports without events, independent source identities and post-commit
dispatch. These staging measurements exclude network scans and discovery matching.

For the actual CSV path, including parsing and inventory/history writes:

```sh
go test ./internal/web -run '^$' -bench '^BenchmarkCSVWebhookStaging$' -benchmem
```

Both variants roll back after the last staged row, preserving the same backlog
between samples. Commit and delivery wake-up are excluded from this measurement.

Measure custom host-type replacement with 1 or 100 hosts and 9,000 pending
deliveries:

```sh
go test ./internal/web -run '^$' -bench '^BenchmarkHostTypeReassignment$' -benchmem
```

This includes host reads, updates, history and webhook staging. Each sample
rolls back after the last host, preserving the inventory and backlog without
sending notifications. Commit and delivery wake-up are excluded.

## Entity selector benchmark

Measure construction of the complete selector list for 10,000 hosts with
1 KiB of notes each:

```sh
go test ./internal/web -run '^$' -bench '^BenchmarkCatalogOptions$' -benchmem
```

This measures option construction, excluding HTML/browser rendering. It still
returns every option. Tests compare labels and order for all 17 types with an
independent oracle based on the original repository reads, including tied values
and composite labels. Store tests cover snapshot consistency, cancellation and
errors.

## Detail relationship benchmark

Measure the shared detail renderer for one relationship in a fixture with
10,000 unrelated hosts:

```sh
go test ./internal/web -run '^$' -bench '^BenchmarkDetailSingleRelationship$' -benchmem
```

This includes tags, relationships, journal/history reads, the neighborhood graph
and HTML rendering. Entity-specific extras such as IPAM, agent facts and rack
elevation are excluded, as are the caller's entity lookup and browser rendering.

## Tag drilldown benchmark

Measure the rendered tag page for one tagged host in an inventory of 10,000
hosts, using a host-only catalog:

```sh
go test ./internal/web -run '^$' -bench '^BenchmarkTagDrilldownSingleReference$' -benchmem
```

The measurement includes tag lookup, label resolution, template rendering and
request/response setup. Database setup is excluded. It does not measure a tag
containing the whole inventory or browser rendering.

## Activity link lookup

Measure existence lookup for five activity references in an inventory of
20,000 hosts, compared with loading the full host inventory:

```sh
go test ./internal/store -run '^$' -bench '^BenchmarkActivityReferenceLookup$' -benchmem
```

The benchmark excludes page rendering and the old catalog's additional label
formatting. Regression tests cover batches larger than 500 IDs, type/ID
namespaces, unknown and deleted references, frozen labels, and storage errors.

## Port generation name lookup

Compare the owner's name query with the previous full port-list lookup in an
inventory of 1,000 owners with 24 ports each:

```sh
go test ./internal/store -run '^$' -bench '^BenchmarkPortGenerationNameLookup$' -benchmem
```

Both cases build the same duplicate-name set inside a write transaction. This
measures the lookup step only, excluding port creation, history and webhooks.

## CSV validation allocations

Measure allocations while rejecting CSV files with wide rows, an early invalid
record, or a late invalid record:

```sh
go test ./internal/web -run '^$' -bench '^BenchmarkCSVValidationMemory$' -benchmem
```

These samples exercise parsing and validation without database writes. The
reported bytes are total allocations per operation, not peak resident memory.
Streaming avoids retaining the raw record list; valid imports still retain a
typed write plan and post-commit events proportional to the number of rows.
Regression tests check error precedence, cancellation, BOM/CRLF/multiline input,
and that no inventory is written before the complete file has been read.

## Integrated inventory and discovery acceptance

The automated suites cover protected agent fields, transactional CSV preview and
stale confirmation, cached discovery deviations, structured interfaces/IPAM,
external backup outcomes, health policy, database snapshots and physical topology.
The browser fixture also renders escaped, long SNMP/LLDP observations from a fake
collector; it never contacts a device. SNMP wire tests use a loopback UDP fixture.

For an isolated deployment rehearsal, use a copied database and explicitly
configure only the integrations being exercised. Confirm real authentication,
create a protected host and a structured interface/address, report an external
backup run, change a health rule, and upload an attachment. Create and verify a
database snapshot, restart to check manifest persistence, then restore the
snapshot into a separate instance. Check the same login, inventory, run history,
health settings and exact attachment bytes. Inventory YAML alone intentionally
does not contain credentials, attachment blobs, operational outcomes or policy.

Real-device acceptance remains separate from loopback tests: verify supported
SNMPv3 credentials, device-specific MIB responses, interface naming and both
physical endpoints before confirming LLDP-derived cable associations. An absent
or incomplete LLDP table must remain an observation limitation, never proof that
inventory ports or cables should be deleted.
