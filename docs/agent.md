[← Documentation index](README.md) · [almanaut](../README.md)

# Inventory agent

`almanaut-agent` is a one-shot binary that reports one machine's own facts —
hardware, OS, kernel, CPU, RAM, disks, and network addresses — to almanaut,
run hourly by a systemd timer. It's the push counterpart to
[auto-discovery](discovery.md) above: use it for machines the server
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

**`ips` is replaced, not merged**, exactly as described under
[API tokens](api.md#api-tokens) below: an out-of-band management address such as
IPMI, iDRAC, or iLO is invisible from inside the OS, so it disappears from
the host record on the very first agent report. That warning applies in
full here — an agent-managed host is exactly where it bites.

## Checking on it

- `systemctl list-timers almanaut-agent.timer` — when it last ran and when
  it runs next.
- `journalctl -u almanaut-agent.service` — the outcome of the last run; a
  success line reads `reported host <id>; changed: <fields>`.

The process exit code doubles as a quick diagnosis without opening the logs:

| Exit code | Meaning |
|---|---|
| `0` | Accepted |
| `1` | Config error — bad flags, or an unreadable/incomplete config file |
| `2` | Rejected — a bad token or a schema version the server doesn't speak; fix the setup, retrying won't help |
| `3` | Unreachable — a transport failure or a `5xx`; transient, the next hourly run retries on its own |
| `4` | Conflict — this agent id is already bound to a different host on the server; see `reset-id` below |

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
