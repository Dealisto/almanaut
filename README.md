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

| Variable                      | Default              | Description                                    |
|-------------------------------|----------------------|------------------------------------------------|
| `ALMANAUT_ADDR`               | `:8080`              | TCP listen address                             |
| `ALMANAUT_DATA_DIR`           | `./data`             | Directory for the SQLite database              |
| `ALMANAUT_DOCKER_SOCKET`      | `/var/run/docker.sock` | Path to the Docker socket for auto-discovery |
| `ALMANAUT_ENABLE_NETWORK_SCAN` | `false`              | Enable the opt-in subnet scan                  |
| `ALMANAUT_SCAN_SUBNET`        | (empty)              | Default subnet (CIDR) pre-filled in the scan form |
| `ALMANAUT_PROXMOX_URL`        | (empty)              | Proxmox VE API base URL (e.g. `https://pve.lan:8006`); enables Proxmox discovery when set with a token |
| `ALMANAUT_PROXMOX_TOKEN`      | (empty)              | Proxmox API token (`user@realm!tokenid=secret`) |
| `ALMANAUT_PROXMOX_INSECURE`   | `false`              | Skip TLS verification for a self-signed Proxmox certificate |

## Auto-discovery

To enable Docker container discovery, mount the Docker socket read-only into the container:

```bash
docker run --rm -p 8080:8080 -v almanaut-data:/data -v /var/run/docker.sock:/var/run/docker.sock:ro ghcr.io/almanaut/almanaut:dev
```

If your Docker socket is at a non-standard path, override it with the `ALMANAUT_DOCKER_SOCKET` environment variable.

Then navigate to **Discover → Docker containers** to scan for containers. Optionally select a host, choose the containers you want to import, and they will be created as Services with an automatic "runs on" relationship to the selected host. Discovery only reads from the socket and creates new Services—it never overwrites your manual data.

### Network scan

To enable network subnet scanning, set `ALMANAUT_ENABLE_NETWORK_SCAN=true`. Optionally set `ALMANAUT_SCAN_SUBNET` to pre-fill the subnet in the scan form.

Then navigate to **Discover → Network scan**, enter a subnet (CIDR) and optional ports, click Scan, pick a host type, select the hosts you want to import, and they will be created. The scan is a lightweight pure-Go TCP-connect probe (a host is considered "live" if at least one probed port is open). Network discovery only ever creates new Hosts and never overwrites your manual data. Subnets larger than 1024 hosts are rejected.

### Proxmox

To enable Proxmox discovery, set both `ALMANAUT_PROXMOX_URL` (e.g. `https://pve.lan:8006`) and `ALMANAUT_PROXMOX_TOKEN`. The token must be a Proxmox API token with read access. To create one:

1. Log in to your Proxmox VE web interface
2. Navigate to **Datacenter → Permissions → API Tokens**
3. Click **Add**
4. Choose a user (e.g. `root`) and token ID
5. Assign the **PVEAuditor** role (or equivalent read-only role) to the token
6. Copy the token in the format `user@realm!tokenid=secret` and set it as `ALMANAUT_PROXMOX_TOKEN`

If your Proxmox server uses a self-signed certificate (the default), set `ALMANAUT_PROXMOX_INSECURE=true` to skip TLS verification.

Then navigate to **Discover → Proxmox**, review the discovered resources, optionally keep "Link VMs/LXC to their Proxmox node" checked to create "runs on" relationships, select what to import, and click Import. Proxmox nodes are imported as `physical` hosts, QEMU VMs as `vm` hosts, and LXC containers as `lxc` hosts. Proxmox discovery only reads and only ever creates new Hosts—it never overwrites your manual data.

## License

MIT (see LICENSE).
