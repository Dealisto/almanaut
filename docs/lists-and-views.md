[← Documentation index](README.md) · [almanaut](../README.md)

# Lists, saved views & bulk editing

Every entity list page shares the same controls: filter and sort it, save that
arrangement under a name, and act on several rows at once.

## Filtering & sorting

Each list page can be filtered and sorted by any of its columns — the entity's
own fields, its tags, and any [custom fields](inventory-model.md#custom-fields)
defined for that type. No per-type configuration is involved: the columns come
from the resource's own field labels, so a newly added field is filterable the
day it exists.

**All of the state lives in the query string**, which is the useful part: a
filtered, sorted list is a plain URL. You can bookmark it, paste it in a ticket,
or send it to a colleague, and they see exactly the same view.

Sorting is numeric-aware where it matters — a column holding `9` and `10` sorts
`10` after `9`, not before it, as a plain string sort would. Values that are not
numbers sort case-insensitively.

A "clear" affordance appears only when a filter or sort is actually applied.

## Saved views (`/views`)

A view you keep coming back to can be saved instead of rebuilt. On a filtered
list, save it under a name and it appears in the sidebar, grouped by entity
type.

Saved views are **per-user**: yours are yours, and another account's views never
show up in your sidebar. Each one stores the query string, so opening a view is
the same as re-applying that filter and sort. You can rename or delete your own
views at any time.

Because a view is just a stored query, a view saved against a filter that no
longer matches anything still opens — it simply lists nothing.

## Bulk actions

Select rows with the checkboxes and apply one action to all of them:

| Action | What it does |
|---|---|
| **Add tag** | Adds one tag to every selected entity |
| **Remove tag** | Removes one tag from every selected entity |
| **Set field** | Overwrites one field on every selected entity, leaving its other fields and custom-field values intact |
| **Delete** | Deletes every selected entity |

Three things are worth knowing before using them:

- **The batch is all-or-nothing.** Every action runs in a single transaction, so
  if one row fails, the whole batch rolls back and nothing is changed. You never
  land halfway through a bulk edit.
- **Every change is recorded per entity.** A bulk edit shows up in each
  entity's own [change history](inventory-model.md#history--journal) and in the
  global `/history` feed, exactly as if you had edited the rows one at a time —
  a bulk delete of forty hosts leaves forty history entries, not one.
- **Webhooks fire per entity, after the commit.** A bulk edit of forty rows
  delivers forty [webhook](integrations.md#outbound-webhooks) events, and only
  once the transaction has actually committed — a rolled-back batch delivers
  nothing at all, so a receiver never sees a change that did not happen.
- **Set field only offers text fields.** The field picker lists the entity's
  string fields only. Numeric, boolean, and multi-value fields (a host's `ips`,
  for instance) are deliberately absent, because writing a single typed value
  across a batch is the kind of operation that is far easier to get wrong than
  to undo.

Bulk actions require a writer role — the toolbar is hidden from viewers, and the
route rejects them regardless. See
[Authentication & access control](authentication.md).

---

**See also:** [The inventory model](inventory-model.md) · [Inventory health](inventory-health.md) · [Authentication & access control](authentication.md)
