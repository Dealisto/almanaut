[← Documentation index](README.md) · [almanaut](../README.md)

# Inventory agent

`almanaut-agent` is a one-shot binary that reports one machine's own facts —
hardware, OS, kernel, CPU, RAM, disks, and network addresses — to almanaut,
run hourly by a systemd timer. It's the push counterpart to
[auto-discovery](discovery.md): use it for machines the server
cannot reach over the network, or where you'd rather not expose the Docker
socket to cover it by discovery instead.

## Install

Releases publish a Linux-only `almanaut-agent_<version>_linux_<arch>.tar.gz`
archive (`amd64` and `arm64`), separate from the main `almanaut` binary. On
each machine to monitor:

1. Create an **`agent`**-scoped token at **API tokens**
   (`/account/tokens`) — report-only, so a leaked token can't read or
   change anything else in the inventory.
2. Download and extract the archive for the machine's architecture into a
   fresh private directory (`tar -C` does not create the target directory,
   and extracting into a shared, world-writable one such as `/tmp` itself
   would let another unprivileged user on the box replace the binary or the
   unit file before the next step installs them as root):

   ```bash
   d=$(mktemp -d) && tar -xzf almanaut-agent_*_linux_amd64.tar.gz -C "$d" && cd "$d"
   ```

3. Install and enable it:

   ```bash
   sudo ./install.sh --server https://almanaut.lan --token alm_xxx
   ```

`install.sh` installs the `almanaut-agent` binary, writes
`/etc/almanaut-agent/config.toml` (root-only, mode 600), installs the
`almanaut-agent.service`/`.timer` units, and enables the timer: the first
report fires 2 minutes after boot, then hourly with up to 5 minutes of
random jitter so a fleet installed from the same script doesn't all hit the
server in the same second. To report once immediately instead of waiting:
`sudo systemctl start almanaut-agent.service`.

Re-running `install.sh` — to pick up a new release, for instance — upgrades
the binary and the units but **never touches an existing config file**: it
prints `keeping the existing /etc/almanaut-agent/config.toml` and leaves the
token alone, since rewriting it silently on every upgrade would be a way to
lose a hand-pasted secret with no obvious cause. `--server`/`--token` can
also be supplied as `ALMANAUT_SERVER_URL`/`ALMANAUT_AGENT_TOKEN` environment
variables — worth doing for the token, since a command-line argument is
visible to every user on the box via `ps` and may end up in shell history.

## What it reports, and what it never touches

Each report replaces the host's `os`, `cpu`, `ram`, `disk`, and `ips` with
whatever the agent observed; a value it couldn't determine (common inside an
LXC container without `lxcfs`) is sent empty rather than guessed, and an
empty value never overwrites an existing one. `name` and `type` are set once
— from the reported hostname and detected virtualization kind — only when a
report creates the host; rename it by hand afterwards and it stays renamed.
Everything else on the host record — `notes`, `status`, `check_address`,
rack placement, tags, relationships, and custom fields — the agent never
writes at all.

OS names follow the [os-release format](https://www.freedesktop.org/software/systemd/man/latest/os-release.html),
including quoted names and escaped characters. `/etc/os-release` takes
precedence; `/usr/lib/os-release` is used only when the former is absent.

CPU and RAM summaries use the lower available value from `/proc` and the
cgroup limit. A container limit above the machine's available resources does
not inflate the inventory; when one memory source is unreadable, the agent
uses the other. Invalid or overflowing memory totals are treated as unknown.

Disk summaries are also treated as unknown if a device size cannot be read,
is malformed, or would overflow the byte count. This preserves the existing
storage inventory instead of replacing it with a partial list. Readable
zero-sized devices, such as empty removable drives, are skipped. Inconsistent
root-filesystem usage (negative usage or usage above total capacity) is ignored.

Virtualization detection uses container markers before examining DMI. If no
positive container marker is available and `/proc/1/environ` cannot be read,
the agent leaves virtualization undetermined and reports only the root
filesystem instead of enumerating potentially shared host disks. This can also
limit disk reporting on physical machines or VMs running with restricted
process-inspection permissions, including an empty capability bounding set.
Root UID alone does not guarantee access to another process's environment.
Existing host types remain unchanged by agent updates; a newly created host
with undetermined virtualization still uses the server's `physical` default
and can be classified manually.

**`ips` is replaced, not merged**, exactly as described under
[API tokens](api.md#api-tokens): an out-of-band management address such as
IPMI, iDRAC, or iLO is invisible from inside the OS, so it disappears from
the host record on the very first agent report. That warning applies in
full here — an agent-managed host is exactly where it bites.

## Checking on it

Flags must precede the command, for example
`almanaut-agent -timeout 10s report` or
`almanaut-agent -state-dir /var/lib/almanaut-agent reset-id`.
Extra arguments are rejected before collecting facts or changing the agent
identity. `almanaut-agent --help` prints usage and exits successfully.

The `-timeout` flag bounds collection and reporting together (30 seconds by
default; it must be positive). If a filesystem or another system read blocks,
the command stops waiting at the deadline, exits with code `3`, and sends no
partial report. The OS operation itself is not cancellable portably; its worker
ends with the one-shot process. Repeated in-process attempts cannot start an
unbounded number of collection workers.

- `systemctl list-timers almanaut-agent.timer` — when it last ran and when
  it runs next.
- `journalctl -u almanaut-agent.service` — the outcome of the last run; a
  success line reads `reported host <id>; changed: <fields>`.

The process exit code doubles as a quick diagnosis without opening the logs:

| Exit code | Meaning |
|---|---|
| `0` | Accepted |
| `1` | Config error — bad flags, or an unreadable/incomplete config file |
| `2` | Rejected — a bad token, unsupported schema, or redirected endpoint; fix the setup, retrying won't help |
| `3` | Transient failure — a transport error, `408`, `429`, or `5xx`; the next hourly run retries on its own |
| `4` | Conflict — this agent id is already bound to a different host on the server; see `reset-id` below |

Configure the final server URL, including its correct HTTP or HTTPS scheme.
The agent refuses redirects so reports and bearer tokens stay at the configured
endpoint.

If reading an error response is interrupted, times out, or ends prematurely,
the agent reports a transient failure (exit `3`) even if `400` or `409` headers
were received. Diagnostics retain the HTTP status and underlying read error
without exposing a partial response body. Complete responses retain their
normal rejection or conflict classification.

The URL must have a hostname, a port between 1 and 65535 when specified, and
no embedded username or password. Supply authentication through `token` or
`ALMANAUT_AGENT_TOKEN`; tokens must contain only printable ASCII without whitespace.
Invalid configuration exits with code `1` before creating an agent identity.
A successful HTTP response containing HTML or malformed JSON exits with code
`2`, identifying a misconfigured endpoint. Error logs omit unstructured response
bodies and redact the configured token and endpoint from structured API errors.
Individual URL path segments reflected by a proxy are also redacted, including
their encoded or quoted forms. Overlapping secrets are matched longest first so
masking a prefix cannot expose the rest of a credential.

## Resolving an ambiguous first report

An unbound agent adopts an existing host only when exactly one unbound record
matches its normalized hostname or reported IP addresses. Multiple matches,
including a name matching one host and an IP matching another, return HTTP
`422` and exit code `2`. No host, binding, or report history is changed.

Only valid global-unicast addresses participate in matching, including private
addresses. Optional CIDR prefix lengths are validated before being removed;
IPv6 zone identifiers are accepted, while malformed prefixes and IPv4 zone
suffixes are ignored. IPv4-mapped IPv6 addresses match their IPv4 equivalents.

Correct the duplicate names or IP assignments in the inventory, then report
again with the same agent identity. `reset-id` does not resolve ambiguity.
An agent already bound to a host keeps using that established binding.

## Recovering from a conflict (`reset-id`)

A `409`/exit `4` means the server already has this agent's id bound to a
different host — the usual cause is a VM cloned or restored from a snapshot
*after* the agent had already written its identity to
`/var/lib/almanaut-agent/agent-id`, so both copies now report under the same
id. Fix it on the **clone**:

```bash
sudo almanaut-agent reset-id
```

This discards the stored id and generates a fresh one; the next report (the
next scheduled run, or `sudo systemctl start almanaut-agent.service` to
force it) then registers as a new host.

A rejected report is not just a `4` in the logs: the host page for the
machine that *kept* the binding shows a **Conflict** banner naming the
hostname that got rejected and when. It means exactly what `reset-id`
above fixes — a different machine reported using this host's agent id; the
original machine keeps the host, and the impostor's reports keep getting
the `409` until it runs `reset-id`. Nothing needs to be cleared by hand
afterwards: the next report accepted from the rightful agent overwrites the
binding and wipes the conflict fields with it, so the banner simply
disappears on its own.

## The agent panel on the host page

Every host's detail page has an **Agent** panel with three states:

- **No agent installed** — the host has no binding at all. This is the
  normal state for anything not running the agent, and links to this page.
- **Bound** — agent id, last-seen time, agent version, kernel, and uptime,
  plus a disk table and a network interface table when the latest report
  included them. These come from two different sources: agent id and
  last-seen are binding facts that always show once a binding exists; the
  rest comes from parsing the most recent raw report, so if that report is
  missing or fails to parse, the binding facts still show and the report
  fields are just blank rather than the whole panel disappearing.
- **Status could not be checked** — the lookup itself failed (for example a
  transient database lock), as distinct from confirming there is no
  binding. The panel says so explicitly and **this does not mean the host
  has no agent** — reload, or check back, rather than assuming the agent
  needs reinstalling.

A host with a binding also gets an **Unbind agent** button (writers only).
Unbinding does not touch the host record itself — only the binding row that
ties it to an agent id — and is the recovery path described next.

## Possible duplicate host records

Separately from a conflict, the panel can also warn about **possible
duplicate host record(s)**: one or more other hosts that share this one's
(normalized) name or an IP address. This is **not** a conflict — it is not
about two machines fighting over one agent id, and it can be raised even
when there is no contested binding at all. It means two separate host
*records* look like the same physical or virtual machine, and it is only
raised when at least one of the two already has an agent binding (two
hand-entered hosts that happen to share a name are the operator's business,
not the agent's).

The concrete cause is almost always one of two things happening on the
machine that already has a bound host record:

- its agent state file, `/var/lib/almanaut-agent`, was lost — a reinstalled
  OS or a restored VM with no persisted state — so the agent starts fresh
  with a brand-new id; or
- `reset-id` was run on the **original** machine instead of the clone.

Either way, the machine now reports under an id the server has never seen,
but its old host record is still marked bound to the previous id. Because
that record still looks "taken", the new report can't adopt it — it creates
a second host with the same name and addresses instead.

## Recovering from a duplicate

The order below matters: unbind before you delete. A bound host is excluded
from adoption candidates, so if the redundant record is deleted first while
the kept record is still bound, the next report finds no name match and
creates a *third* host instead of re-adopting the one you kept.

1. On the **record you want to keep** (typically the older one, with the
   history you care about), click **Unbind agent**. This only removes the
   stale binding; the host record, its tags, notes, and history are
   untouched.
2. Delete the redundant record the agent just created.
3. Do nothing else — the agent's *next* report (the next scheduled run, or
   `sudo systemctl start almanaut-agent.service` to force it now) is now
   unbound, matches the kept record by hostname or IP the same way a
   first-ever report would, and re-adopts it.

---

**See also:** [Auto-discovery](discovery.md) · [JSON API](api.md) · [Installation](installation.md)
