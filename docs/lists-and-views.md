[← Documentation index](README.md) · [almanaut](../README.md)

# Lists, saved views & bulk editing

Every entity list page shares the same controls: filter and sort it, save that
arrangement under a name, and act on several rows at once.

## Filtering & sorting

Use **Find in this list** to search names, addresses, notes, and custom-field
values without leaving the current entity type. Searches ignore letter case.
Combine that search with a tag, a sort order, or **Filter a specific field**,
then select **Apply**. The tag input offers up to 100 suggestions; you can type
any exact tag name, including one outside those suggestions. The header's global
search looks across all entity types;
press **Ctrl+K** (or **Cmd+K**) to focus it.

Each list page can be filtered and sorted by its available field columns,
including [custom fields](inventory-model.md#custom-fields) defined for that
type. The separate tag filter can be combined with those controls.

**All of the state lives in the query string**, which is the useful part: a
filtered, sorted list is a plain URL. You can bookmark it, paste it in a ticket,
or send it to a colleague, and they see exactly the same view.

Sorting is numeric-aware where it matters — a column holding `9` and `10` sorts
`10` after `9`, not before it, as a plain string sort would. Values that are not
numbers sort case-insensitively.
Large integers and close decimal values keep their exact numeric order, even
when a machine floating-point value would round them to the same number.
When a column mixes numbers and text, ascending order places numbers first;
descending order reverses both groups. `NaN` is treated as text.

A "clear" affordance appears only when a filter or sort is actually applied.

## Pagination

Lists and global search show **50 records per page** by default. Entity lists
also offer a rows-per-page selector; pages accept up to **100 records**. The
count and Previous/Next links preserve the active filters and sort order.
Filtering, sorting, and page selection happen in the database before records
are loaded, so a large inventory does not need to be copied into application
memory to render one page. Substring filters may still scan matching tables.

If a bookmarked page number is beyond the remaining results, the list offers
a return to the first page while preserving the current filters, sort order,
and page size. This is distinct from a search with no matching records.

Opening a record from a list preserves that list's search, filters, sorting,
page and page size in the record link. **Back to list** returns to that view,
including after switching detail tabs or opening the record in a new tab.
This works without JavaScript. Only controls for the same entity list are
accepted as a return destination. **New** and **Edit** carry the same context
through validation errors, **Cancel**, and successful saves. Bulk port generation
preserves this context after invalid input and successful generation. Adding or
removing a tag, journal entry or attachment also retains the originating view;
journal and attachment actions keep the Activity tab selected. Following
relationships to other records starts a separate navigation path without this
list context.

## Saved views (`/views`)

A view you keep coming back to can be saved instead of rebuilt. On a filtered
list, open **Save view**, give it a name, and it appears in the sidebar, grouped by entity
type.

Apply your filters before saving: a saved view captures the filters, sort order,
and page size. It opens at the first page instead of retaining the current page
number.

Saved views are **per-user**: yours are yours, and another account's views never
show up in your sidebar. Each one stores the query string, so opening a view is
the same as re-applying that filter and sort. You can rename or delete your own
views at any time.

Because a view is just a stored query, a view saved against a filter that no
longer matches anything still opens — it simply lists nothing.

## Bulk actions

After an action, the list keeps its filters, sort order, and current page.

Select rows with the checkboxes, open **Bulk actions**, and apply one action to
all of them. The header checkbox selects every row on the current page;
the selection count shows how many records the action will affect. Individual
rows keep their edit and delete controls under **Actions**.

Selections, the action, and its parameters come only from the submitted form
body. Query-string values cannot add unchecked records or supply an action.

| Action | What it does |
|---|---|
| **Add tag** | Adds one tag to every selected entity |
| **Remove tag** | Removes one tag from every selected entity |
| **Set field** | Overwrites one field on every selected entity, leaving its other fields and custom-field values intact |
| **Delete** | Deletes every selected entity |

Things to know before using them:

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
