[← Documentation index](README.md) · [almanaut](../README.md)

# JSON API

A read-write JSON API mirrors the inventory for scripts and dashboards.
**Reads** (`GET`) accept either a logged-in session cookie or an API token.
**Writes** (`POST`/`PUT`/`DELETE`) require an API token — a request carrying
only a session cookie is rejected, so a browser can never trigger a write via
a plain form post. Any request missing valid credentials gets a plain
`401 {"error":"…"}` instead of the UI's redirect to `/login`.

## API tokens

Create a personal token at **API tokens** (`/account/tokens`) while logged in:
give it a label and a **scope** — `read-write`, `read-only`, or `agent`. A
request's effective permission is the intersection of the token's scope and
its owner's [role](authentication.md#roles) (a viewer's token can never write,
and a read-only token can't write even for an admin).

An **`agent`**-scoped token is report-only: it authenticates nothing but
`POST /api/agent/report` (see below) and cannot read the inventory or mutate
any entity, regardless of its owner's role. Because the intersection rule
above still applies, the token's owner also needs a role that can write
(`admin` or `editor`) — a viewer's token cannot report, even scoped `agent`.
Issue one per host running [`almanaut-agent`](agent.md); it never needs read access to
the rest of the API. A report never overwrites `notes`, `status`,
`check_address`, rack placement, tags, relationships or custom fields — it
only ever updates `os`, `cpu`, `ram`, `disk` and `ips`. `name` and `type` are
the exception: both are set once, from the reported hostname and virtualization
kind, only when the report creates the host; a host renamed by hand (or
re-typed) afterwards keeps that value forever, since later reports never touch
either field again.

**`ips` is replaced, not merged.** Each report overwrites the host's address
list with exactly what the agent saw, after dropping loopback and link-local
addresses. Anything the machine cannot see from inside its own OS therefore
disappears from the record on the first report — most commonly an out-of-band
management address such as IPMI, iDRAC or iLO. That matters beyond the host
page: `host.ips` feeds IPAM attribution, so a dropped BMC address will show as
free capacity and could be handed out as the next free address while the
interface is live. If you track such addresses, keep them on a separate host
record, or as a reservation on the network, rather than on the agent-managed
host.

The raw token (`alm_...`) is shown **once**, right after creation — copy it
then, since only its hash is stored. Revoke a token from the same page at any
time; each user only sees and can revoke their own tokens.

Send the token as a bearer header:

```bash
curl -X POST http://localhost:8080/api/hosts \
  -H "Authorization: Bearer alm_..." \
  -H "Content-Type: application/json" \
  -d '{"name":"nas","type":"physical"}'
```

## Endpoints

| Endpoint | Returns |
|---|---|
| `GET /api/{type}` | All entities of a type (e.g. `/api/hosts`, `/api/hardware`, `/api/certificates`) |
| `POST /api/{type}` | Create an entity from a JSON body; `201` + `Location` header + the created entity, or `400` on validation/malformed-JSON errors |
| `GET /api/{type}/{id}` | One entity, or `404 {"error":"…"}` if absent |
| `PUT /api/{type}/{id}` | Full replace from a JSON body (not a partial patch); `200` + the updated entity, or `404`/`400` |
| `DELETE /api/{type}/{id}` | `204` on success, `404` if absent |
| `GET /api/search?q=<term>` | Flat array of matches: `[{"type","id","label","path"}]` |
| `GET /api/relationships` | All relationships |
| `GET /api/openapi.json` | The OpenAPI 3 document describing every route and schema |
| `POST /api/agent/report` | Ingest one report from `almanaut-agent`. Requires an **`agent`**-scoped token. `200` + `{"host_id","changed"}`, `400` on malformed JSON/failed validation/unknown `schema_version`, `409` when the agent id is bound to a different machine |

`{type}` bases mirror the web UI's routes, not a naive plural of the entity
name — `hardware`, not `hardwares` (see [The inventory model](inventory-model.md)
for the full set). Field names match the YAML export (snake_case), and request
bodies use the same shape. Responses are `application/json`.

Every API write is recorded in the same per-entity **change history** as UI
edits, attributed to the token's owning user — so `GET /history` and each
entity's Change history show exactly who made a scripted change.

```bash
curl -s http://localhost:8080/api/certificates \
  -H "Authorization: Bearer alm_..." | jq '.[] | {subject, expires_on}'
```

## API reference

A browsable reference lives at **API docs** (`/api/docs`) — every endpoint and
entity schema, rendered server-side (no external JS). The same information is
served as a machine-readable [OpenAPI 3](https://spec.openapis.org/oas/v3.0.3)
document at `/api/openapi.json`, suitable for client generators or import into
tools like Postman. Both are generated from the entity catalog, so they always
match the running build.

---

**See also:** [Authentication & access control](authentication.md) · [The inventory model](inventory-model.md) · [Inventory agent](agent.md) · [Monitoring](monitoring.md)
