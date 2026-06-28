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

| Variable                  | Default              | Description                                    |
|---------------------------|----------------------|------------------------------------------------|
| `ALMANAUT_ADDR`           | `:8080`              | TCP listen address                             |
| `ALMANAUT_DATA_DIR`       | `./data`             | Directory for the SQLite database              |
| `ALMANAUT_DOCKER_SOCKET`  | `/var/run/docker.sock` | Path to the Docker socket for auto-discovery |

## Auto-discovery

To enable Docker container discovery, mount the Docker socket read-only into the container:

```bash
docker run --rm -p 8080:8080 -v almanaut-data:/data -v /var/run/docker.sock:/var/run/docker.sock:ro ghcr.io/almanaut/almanaut:dev
```

If your Docker socket is at a non-standard path, override it with the `ALMANAUT_DOCKER_SOCKET` environment variable.

Then navigate to **Discover → Docker containers** to scan for containers. Optionally select a host, choose the containers you want to import, and they will be created as Services with an automatic "runs on" relationship to the selected host. Discovery only reads from the socket and creates new Services—it never overwrites your manual data.

## License

MIT (see LICENSE).
