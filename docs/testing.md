# Testing

Run the normal Go checks from the repository root:

```sh
go vet ./...
go test ./...
go test -race ./...
```

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

- Detail-tab selection, keyboard navigation, modified clicks, and browser history.
- Revealing a hidden panel and scrolling to a deep-link heading.
- Bulk-action state when `pageshow` follows restored checkbox values, including
  partial selection and clearing all rows. The restoration event is simulated;
  this does not assert a browser's cache admission policy.
- Human-readable selection labels for all 17 inventory types, including quotes,
  ampersands, and HTML-looking names where domain validation permits them.
- Distinct empty-search and out-of-range-search messages, with a working
  first-page URL that preserves the query.
- Mobile navigation focus, keyboard trapping, Escape, and search shortcuts on
  the real hosts page, including switching from desktop to a narrow viewport.
- Print-media styles with the mobile drawer open, so its overlay and scroll
  lock cannot obscure the printed inventory.

Every assertion is printed as `PASS` or `FAIL`; a failed assertion, server error,
browser startup failure, or timeout exits nonzero. The runner stops its processes
and deletes the temporary database/profile on completion, failure, or interruption.
The complete run has a 120-second deadline. No personal browser profile is used.

These focused checks complement the Go suite. They do not replace visual review,
screen-reader testing, or cross-browser compatibility testing.

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
