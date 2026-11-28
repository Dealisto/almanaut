[← Documentation index](README.md) · [almanaut](../README.md)

# Podman discovery

Almanaut discovers running Podman containers through Podman's Docker-compatible
API. Use **Discover → Docker / Podman containers**: review the proposed
services, optionally select their host, and import. The result page shows added,
existing and skipped records plus host links created. Imports preserve existing
service fields and remember each confirmed container's source identity.

The same connector supports Docker and Podman. Its existing names remain
compatible: `ALMANAUT_DOCKER_SOCKET` selects the socket,
`ALMANAUT_DISCOVERY_DOCKER_INTERVAL` enables scheduled discovery, and the page
stays at `/discovery/docker` for the default source. Scheduled discovery records
findings for review; it does not automatically import them. Each configured
socket inventories one engine and, for rootless Podman, one user's containers.
Use [multiple discovery sources](configuration.md#multiple-discovery-sources) to
inventory several engines or users with separate sockets and schedules.

Renaming a container or its Almanaut record preserves an existing association.
Recreating a container gives it a different external ID. If its name still
matches an already-associated record from the same source, the preview reports
**Name or identity conflict** and skips it. Discovery neither creates a silent
duplicate nor reassigns the old association. Use
[Resolve replacements](discovery.md#replacing-a-source-identity) to review a
proposed identity replacement while preserving the inventory record and its
relationships. Proposals require an unambiguous match and the old ID to be absent
from the scan; a stopped container is also absent, so check the engine before
confirming. A same-name record associated with a different source does not cause
this conflict.

Unassociated manual records can be matched by name and associated only after
confirmation. See [persistent identity](discovery.md#sources-and-persistent-identity)
for matching and ambiguity rules.

## Select the socket

For **rootless Podman**, enable the socket as the user who owns the containers:

```bash
systemctl --user enable --now podman.socket
```

When running the Almanaut binary as that user, set:

```bash
ALMANAUT_DOCKER_SOCKET="$XDG_RUNTIME_DIR/podman/podman.sock" ./almanaut
```

The usual expanded path is `/run/user/1000/podman/podman.sock`; use your own
user ID, not that example value. A background service needs the expanded
absolute path in its environment. Rootless discovery sees that user's engine,
not the separate root-owned engine.

For **rootful Podman**, enable the system socket:

```bash
sudo systemctl enable --now podman.socket
```

Set `ALMANAUT_DOCKER_SOCKET=/run/podman/podman.sock` in Almanaut's environment.
The account running Almanaut must have permission to connect to that socket.

## Run Almanaut in a container

Mount the chosen host socket at a path inside the Almanaut container and set
`ALMANAUT_DOCKER_SOCKET` to that **container-side path**. For example, the
relevant options for a rootless Podman deployment are:

```bash
-v "$XDG_RUNTIME_DIR/podman/podman.sock:/run/container-engine.sock:ro"
-e ALMANAUT_DOCKER_SOCKET=/run/container-engine.sock
```

These are options to add to your existing container command. The container's
user and user-namespace mapping must permit access to the socket; the default
non-root image user does not automatically inherit the host user's access.
On SELinux hosts, Podman documents `--security-opt label=disable` for socket
access from a container. Do not relabel the engine socket.

Almanaut only requests the container list. A socket mounted with `:ro` still
allows API requests, including writes by other clients with access to it;
the mount flag does not make the engine API read-only. Keep socket access
restricted rather than exposing an unauthenticated network listener.

## Imported details and limits

- Each running container becomes a Service with its name, published ports,
  image provenance, and Compose project as its category when available.
- The standard `com.docker.compose.project` label takes precedence, with
  `io.podman.compose.project` as a fallback. Containers without either label
  have an empty category.
- Repeated IPv4/IPv6 bindings for the same published port appear once.
- Pods, images, volumes, stopped containers, and the engine host itself are
  not imported by this connector. Select an existing host during import to
  create the service's "runs on" relationship.

See the [Podman service documentation](https://docs.podman.io/en/latest/markdown/podman-system-service.1.html)
for socket activation, paths, and permissions, and
[auto-discovery](discovery.md) for the other supported sources.
