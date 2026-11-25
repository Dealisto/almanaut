package web

import "github.com/Dealisto/almanaut/internal/store"

// listControls is the filter/sort state of a list page, rendered by the shared
// "listcontrols" partial and used to build shareable query-string URLs. All
// state lives in query params so a filtered/sorted list is a plain link.
type listControls struct {
	BasePath     string // e.g. "/hosts"
	Type         string // singular type key, e.g. "host" (for saving a view)
	CurrentQuery string // filters and page size, excluding current page (for saving a view)
	Query        string // case-insensitive search across this resource's fields

	Columns               []string // filterable/sortable column labels ("Name" + fields + custom fields)
	Tags                  []string // up to 100 tag-name suggestions for this entity type
	TagSuggestionsLimited bool
	Sort                  string // current sort column ("" = none)
	Dir                   string // "asc" or "desc"
	Tag                   string // current tag filter ("" = none)
	FilterField           string // current filter column ("" = none)
	FilterValue           string // current filter substring
	Pagination            pagination
	SetFields             []string // string field keys offered by the bulk "set field" action
}

// Active reports whether any filter or sort is applied, so the template can show
// a "clear" affordance only when it does something.
func (c listControls) Active() bool {
	return c.Query != "" || c.Sort != "" || c.Tag != "" || (c.FilterField != "" && c.FilterValue != "")
}

// compareValues orders numbers numerically before case-insensitive text.
// Classifying each operand independently keeps mixed columns transitive;
// comparing mixed pairs lexically would produce cycles such as 2 < 10 < 1x < 2.
// NaN is text because it cannot participate in an ordered numeric comparison.
// Returns -1, 0, or 1.
func compareValues(a, b string) int { return store.CompareInventoryValues(a, b) }
