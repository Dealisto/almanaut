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

Choose an optional lifetime of 7, 30, 90, or 365 days, or **No expiration**.
The form defaults to 90 days. Expiration is enforced on every bearer request;
expired tokens return `401` and remain visible until revoked. Existing tokens
keep their previous no-expiration behavior after upgrading. The list displays
expiry and last-used times; last use is persisted at most once every five
minutes per token, with the first use recorded immediately.

To rotate without interrupting an integration:

1. Open **Rotate token** on the original token and choose the replacement's
   lifetime. **Prepare replacement** creates a token with the same scope,
   label, and owner, while the original remains valid.
2. Copy the replacement once and deploy it to every integration using the
   original. Check that the integration works and refresh the token page.
3. After the replacement has authenticated a request, **Retire original token**
   becomes available. Confirming it revokes the original atomically. The
   replacement must still be unexpired.

Only one replacement can be pending for an original. If you lose the newly
shown secret, revoke that replacement and prepare another; the original is
unaffected. API tokens are excluded from inventory exports.

Send the token as a bearer header:

```bash
curl -X POST http://localhost:8080/api/hosts \
  -H "Authorization: Bearer alm_..." \
  -H "Content-Type: application/json" \
  -d '{"name":"nas","type":"physical"}'
```

Host `type` values must be names from `GET /api/host-types`, including any
custom types created through **Settings & data → Host types**. Send the stable
`name`, not its display `label`. An unknown name returns `400` on create or
update. Host JSON continues to use the same string `type` field.

## Endpoints

| Endpoint | Returns |
|---|---|
| `GET /api/{type}` | A bounded page of entities of a type (e.g. `/api/hosts`, `/api/hardware`, `/api/certificates`) |
| `POST /api/{type}` | Create an entity from a JSON body; `201` + `Location` header + the created entity, or `400` on validation/malformed-JSON errors |
| `GET /api/{type}/{id}` | One entity, or `404 {"error":"…"}` if absent |
| `PUT /api/{type}/{id}` | Full replace from a JSON body (not a partial patch); `200` + the updated entity, or `404`/`400` |
| `DELETE /api/{type}/{id}` | `204` on success, `404` if absent |
| `GET /api/host-types` | Available host types as `[{"name":"vm","label":"Virtual machine"}, …]` |
| `GET /api/search?q=<term>` | A bounded page of matches: `[{"type","id","label","path"}]` |
| `GET /api/relationships` | All relationships |
| `GET /api/openapi.json` | The OpenAPI 3 document describing every route and schema |
| `POST /api/agent/report` | Ingest one report from `almanaut-agent`. Requires an **`agent`**-scoped token. `200` + `{"host_id","changed"}`, `400` on malformed JSON/failed validation/unknown `schema_version`, `409` when the agent id is bound to a different machine, `422` when several unbound hosts match its name or IPs |

`{type}` bases mirror the web UI's routes, not a naive plural of the entity
name — `hardware`, not `hardwares` (see [The inventory model](inventory-model.md)
for the full set). Field names match the YAML export (snake_case), and request
bodies use the same shape. Responses are `application/json`.

Request bodies are limited to 32 MiB. Exceeding the limit returns
`413 {"error":"request body too large"}`, including chunked uploads.

Numeric values in `custom_fields` retain their submitted precision in storage
and JSON responses. Clients that need exact large integers or decimal values
should use a JSON parser that preserves numbers instead of converting them to
binary floating point.

Every API write is recorded in the same per-entity **change history** as UI
edits, attributed to the token's owning user — so `GET /history` and each
entity's Change history show exactly who made a scripted change.

```bash
curl -s http://localhost:8080/api/certificates \
  -H "Authorization: Bearer alm_..." | jq '.[] | {subject, expires_on}'
```

## Pagination and filtering

Entity collection endpoints and `/api/search` return **50 records per page by
default**, with a maximum of **100**. The JSON body remains an array. Existing
clients that need the full inventory must follow the `Link` header's `next`
relation until it is absent; a single request no longer returns an unlimited
collection. Whole-inventory YAML exports remain complete.

- `page`: one-based page number, default `1`.
- `per_page`: page size from `1` to `100`, default `50`.
- `X-Total-Count`: matching records across all pages.
- `X-Page` and `X-Per-Page`: the current page and its configured size.
- `Link`: relative `first`, `prev`, and `next` links preserving query parameters.

Invalid pagination parameters return `400`. A page beyond the last match
returns `[]` with the total count and links back to existing pages. Ordering is
stable for unchanged data, with entity IDs breaking ties. Pages are read from a
consistent database snapshot; inventory changes between separate requests can
still move records between pages.

Entity lists accept the same `q`, `tag`, `field`, `value`, `sort`, and `dir`
parameters as the browser UI. Field and sort names are visible labels such as
`Name`, `Rack`, or a custom field's label. Text matching is Unicode
case-insensitive; substring searches treat `%` and `_` literally. Search also
matches custom values and tags, and keeps results grouped in catalog order.

```bash
curl -i 'http://localhost:8080/api/hosts?per_page=25&page=1&sort=Name' \
  -H "Authorization: Bearer alm_..."
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
