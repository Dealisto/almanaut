[← Documentation index](README.md) · [almanaut](../README.md)

# Installation

Every way to get almanaut running: Docker, Docker Compose, a prebuilt
binary, or from source — then the first-login flow and the optional sample
inventory. Every `ALMANAUT_*` variable used below is described in
[Configuration](configuration.md).

## Docker

```bash
docker run --rm -p 8080:8080 -v almanaut-data:/data ghcr.io/dealisto/almanaut:dev
```

Open http://localhost:8080 and log in with the admin credentials printed in
the container log (see [First login](#first-login)).

Images are published to GHCR automatically: `:dev` tracks `master`, and a
tagged release (`vX.Y.Z`) publishes `:X.Y.Z`, `:X.Y`, and `:latest`. Images
are multi-arch (`linux/amd64` and `linux/arm64`).

The container runs as a non-root user (uid `65532`). A fresh named volume (as
above) inherits the right ownership automatically. If you instead bind-mount a
host directory (`-v /host/path:/data`), make it writable by that uid first —
`sudo chown 65532:65532 /host/path` — otherwise the database cannot be created.

## Docker Compose

Drop this into `docker-compose.yml` and run `docker compose up -d`:

```yaml
services:
  almanaut:
    image: ghcr.io/dealisto/almanaut:dev
    container_name: almanaut
    ports:
      - "8080:8080"
    volumes:
      - almanaut-data:/data
      # Uncomment to enable Docker container auto-discovery (read-only):
      # - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      # All optional — see docs/configuration.md. A few common ones:
      # ALMANAUT_AUTH_USER: admin        # seeds the initial admin account
      # ALMANAUT_AUTH_PASS: change-me    # seeds the initial admin account
      # ALMANAUT_ENABLE_NETWORK_SCAN: "true"
      # ALMANAUT_NTFY_URL: https://ntfy.sh/my-homelab
      TZ: Etc/UTC
    restart: unless-stopped

volumes:
  almanaut-data:
```

The image ships its own `HEALTHCHECK`, so `docker compose ps` reports the
container's health directly.

## Prebuilt binaries

Each tagged release has a [GitHub Release](https://github.com/Dealisto/almanaut/releases)
with prebuilt, statically linked binaries for Linux, macOS, and Windows
(`amd64` and `arm64`), plus a `checksums.txt`. Download the archive for your
platform, verify it, and run it:

```bash
sha256sum -c checksums.txt --ignore-missing
tar xzf almanaut_1.0.0_linux_amd64.tar.gz
ALMANAUT_DATA_DIR=./data ./almanaut
```

Release channels at a glance:

| Channel | What you get |
|---|---|
| Container `:X.Y.Z` / `:X.Y` / `:latest` | Multi-arch images on GHCR for a tagged release |
| Container `:dev` | Rolling image built from `master` |
| GitHub Release binaries | Versioned archives + checksums for a tagged release |

See [CHANGELOG.md](../CHANGELOG.md) for what changed in each release.

## From source

Requires Go 1.26.8 or newer — the version pinned by the `go` directive in
`go.mod`. With the default `GOTOOLCHAIN=auto`, an older Go downloads the right
toolchain itself; with `GOTOOLCHAIN=local` it refuses to build instead.

```bash
go build -o almanaut .
ALMANAUT_DATA_DIR=./data ./almanaut
```

## First login

On first startup, if the user table is empty, almanaut creates one admin
account:

- If `ALMANAUT_AUTH_USER` / `ALMANAUT_AUTH_PASS` are set, it seeds the admin
  with that username/password (username defaults to `admin` if only the
  password is set).
- Otherwise it creates username `admin` with a **random password printed once
  to the server log**, as a banner that looks like this:

  ```
  ========================================================
  Almanaut created an initial admin account.
    username: admin
    password: 7f3kQ9z...
  Log in and change it. This is shown only once.
  ========================================================
  ```

  Copy that password immediately — it is not stored anywhere in recoverable
  form and is never logged again.

## Try it with sample data

Want to see the app populated before entering your own data? This repo ships a
small example homelab (three hosts, some services, a network, a certificate, a
backup, and the relationships between them). Grab
[`examples/inventory.yaml`](../examples/inventory.yaml), then go to **Data →
Import**, upload it, tick the confirmation box, and import. You'll land on a
populated dashboard with a browsable relationship graph.

Since import wipes existing data, only load the sample into a fresh instance
(or export your real data first).

---

**See also:** [Configuration](configuration.md) · [Authentication & access control](authentication.md) · [Export & import](export-import.md)
