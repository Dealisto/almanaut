[← Documentation index](README.md) · [almanaut](../README.md)

# Auto-discovery

Discovery reads external systems without changing them. You review the results
and confirm which records to add or associate with existing inventory. Manual
names and details are never overwritten. Configuration is described in
[Configuration](configuration.md); for machines the server cannot reach at all,
see the [inventory agent](agent.md) instead.

## Sources and persistent identity

You can configure multiple Docker/Podman engines, Proxmox clusters and Xen
Orchestra instances using [a discovery source file](configuration.md#multiple-discovery-sources).
Each appears separately on **Discover**, with its own review page and schedule.
The existing single-source environment variables still work when no source
file is configured.

Each imported resource is associated with its source's stable `type:id` key
and external resource identifier. That association survives restarting Almanaut,
renaming the inventory record, or renaming the resource in its source. Changing
a source's display name or endpoint also preserves it, provided its `type` and
`id` remain unchanged. Renames do not automatically overwrite inventory names.

Before an association exists, a unique name match can identify a manually
entered record. Names are compared without case or surrounding whitespace;
host types must also match. The preview marks this **Existing record: confirm
identity**. Selecting it and importing saves the association without replacing
its fields. Previewing or scheduling discovery never creates that association.

Already-associated records are matched by identity, not by name. Distinct
external identifiers can create separate records with the same name when no
existing association or manual-record match conflicts. A record associated with
another source neither suppresses a new record nor gets silently adopted.

Within one source, a new external identifier matching the name of an
already-associated local record is reported as **Name or identity conflict**.
For example, a recreated container may retain its name but acquire a new ID.
That selection is skipped rather than silently duplicated or reassigned. If
several candidates compete for an unassociated manual record, or several local
records match its name, the selection is also ambiguous. Review the conflicting
records before importing again.

Deleting a host or service also removes its discovery association. A later
import can create it again. [YAML exports](export-import.md) preserve these
associations but do not contain source credentials or configuration.

## Import results

Container and hypervisor imports fetch current source data again, then save the
selected records, associations and host links in one transaction. Resources
that disappeared since the preview are skipped. The result page reports:

- **Added**: newly created inventory records.
- **Already present**: selected records already associated with the source,
  or existing manual records whose identity was confirmed.
- **Skipped**: selected identifiers that were missing, invalid or ambiguous.
- **Host links created**: new service-to-host or guest-to-host relationships.

Counts cover the selected resources only. Existing records and their
relationships remain unchanged; optional host links are added for newly created
records. Confirming guest-to-host links can also save the association of an
unambiguous existing parent without changing that parent's fields. Use **View
inventory** or **Review source again** to continue.

The import form returns the result page directly with HTTP `200`, rather than
redirecting to the inventory list. Refreshing it may ask the browser to resend
the form; repeated imports recognize the saved identities and do not create
duplicate records. Network-scan imports retain their existing workflow.

## Docker containers

Mount the Docker socket read-only into the container:

```bash
docker run --rm -p 8080:8080 -v almanaut-data:/data \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  ghcr.io/dealisto/almanaut:dev
```

(Override a non-standard socket path with `ALMANAUT_DOCKER_SOCKET`.)

Then navigate to **Discover → Docker / Podman**. Optionally select a host,
choose the containers to import, and they are created as Services with an
automatic "runs on" relationship to the selected host.

## Podman containers

Podman uses the same discovery page through its Docker-compatible API. Set
`ALMANAUT_DOCKER_SOCKET` to the Podman socket visible to Almanaut. Rootful and
rootless engines have separate inventories; choose the socket for the user
running your containers. See [Podman discovery](podman.md) for setup and
container deployment examples.

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

Proxmox resource identifiers use the node name or guest VMID (for example,
`qemu/100`); they do not identify a particular generation of a guest. If a guest
is deleted in Proxmox and its VMID reused for a different machine, Almanaut
cannot distinguish the replacement from the previously associated resource.
Review and remove the obsolete local record before importing the replacement;
deleting that local record releases its association. This does not delete the
machine in Proxmox.

## XCP-ng via Xen Orchestra

Configure the Xen Orchestra server that manages your XCP-ng hosts:

```bash
export ALMANAUT_XO_URL=https://xo.lan
export ALMANAUT_XO_TOKEN_FILE=/run/secrets/xo_token
# Optional: discover new resources every six hours for manual review.
export ALMANAUT_DISCOVERY_XO_INTERVAL=6h
```

Use the XO base URL, not an individual hypervisor's address. Create a token
with `xo-cli create-token xo.lan inventory@example.com` and save the returned
token in the configured file. The client authenticates using the
`authenticationToken` cookie and reads the `/rest/v0/hosts` and `/rest/v0/vms`
collections. See the [official REST API documentation](https://docs.xen-orchestra.com/automation/restapi).
Use an account with permission to read the intended hosts and VMs; available
permissions depend on your XO version. Verify both collections are accessible.
Renew expiring tokens in the token file and restart Almanaut to load changes.

TLS verification is enabled by default. For a self-signed endpoint,
`ALMANAUT_XO_INSECURE=true` disables verification; prefer a trusted certificate.

Open **Discover → XCP-ng / Xen Orchestra**, review the resources, select the
rows to import, and optionally link VMs to their hosts. Hosts become `physical`
records and VMs become `vm` records. CPU, memory and valid IP addresses are
included when available; missing facts remain blank. VM templates, snapshots
and control domains are excluded. Disk inventory is not collected.

Resources are tracked by their XO UUID within the configured source. Distinct
resources may share a name, subject to the identity-conflict and manual-record
matching rules above. Review and import both fetch current data, so resources
removed since the preview are not imported.

Host relationships use the VM's current XO host identifier. A stopped VM can
belong to a pool without an identified host; it is imported without a host
relationship. Existing hosts are linked only when the match is unambiguous.
This is inventory discovery, not ongoing synchronization: manual edits are
preserved, and subsequent VM moves do not automatically rewrite relationships.
A resource renamed in XO remains associated with its existing Almanaut record.

---

**See also:** [Inventory agent](agent.md) · [Background jobs](background-jobs.md) · [Configuration](configuration.md) · [The inventory model](inventory-model.md)
