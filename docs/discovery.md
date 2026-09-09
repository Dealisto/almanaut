[← Documentation index](README.md) · [almanaut](../README.md)

# Auto-discovery

All discovery sources are read-only and additive: they only ever **create**
new records and never overwrite your manual data. The variables that enable
them are described in [Configuration](configuration.md); for machines the
server cannot reach at all, see the [inventory agent](agent.md) instead.

## Docker containers

Mount the Docker socket read-only into the container:

```bash
docker run --rm -p 8080:8080 -v almanaut-data:/data \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/dealisto/almanaut:dev
```

(Override a non-standard socket path with `ALMANAUT_DOCKER_SOCKET`.)

Then navigate to **Discover → Docker containers**. Optionally select a host,
choose the containers to import, and they are created as Services with an
automatic "runs on" relationship to the selected host.

## Network scan

Set `ALMANAUT_ENABLE_NETWORK_SCAN=true` (and optionally `ALMANAUT_SCAN_SUBNET`
to pre-fill the form). Then navigate to **Discover → Network scan**, enter a
subnet (CIDR) and optional ports, and import the live hosts you select. The
scan is a lightweight pure-Go TCP-connect probe (a host is "live" if at least
one probed port is open). Subnets larger than 1024 hosts are rejected.

## Proxmox VE

Set `ALMANAUT_PROXMOX_URL` (e.g. `https://pve.lan:8006`) and
`ALMANAUT_PROXMOX_TOKEN`. The token needs read access; to create one:

1. In the Proxmox web UI, go to **Datacenter → Permissions → API Tokens**
2. Click **Add**, choose a user and token ID
3. Assign the **PVEAuditor** role (or an equivalent read-only role)
4. Copy the token as `user@realm!tokenid=secret` into `ALMANAUT_PROXMOX_TOKEN`

For a self-signed Proxmox certificate (the default), set
`ALMANAUT_PROXMOX_INSECURE=true`.

Then navigate to **Discover → Proxmox**, review the discovered resources,
optionally keep "Link VMs/LXC to their Proxmox node" checked to create
"runs on" relationships, and import. Proxmox nodes become `physical` hosts,
QEMU VMs become `vm` hosts, and LXC containers become `lxc` hosts.

---

**See also:** [Inventory agent](agent.md) · [Configuration](configuration.md) · [The inventory model](inventory-model.md)
