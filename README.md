# Almanaut

A lightweight, self-hosted homelab inventory & documentation tool.
"NetBox for the rest of us."

> Status: early development (v0.1).

## Run with Docker

```bash
docker run --rm -p 8080:8080 -v almanaut-data:/data ghcr.io/almanaut/almanaut:dev
```

Then open http://localhost:8080.

## Run from source

```bash
go build -o almanaut .
ALMANAUT_DATA_DIR=./data ./almanaut
```

## Configuration

| Variable             | Default  | Description                       |
|----------------------|----------|-----------------------------------|
| `ALMANAUT_ADDR`      | `:8080`  | TCP listen address                |
| `ALMANAUT_DATA_DIR`  | `./data` | Directory for the SQLite database |

## License

MIT (see LICENSE).
