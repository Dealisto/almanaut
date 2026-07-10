package web

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// listControls is the filter/sort state of a list page, rendered by the shared
// "listcontrols" partial and used to build shareable query-string URLs. All
// state lives in query params so a filtered/sorted list is a plain link.
type listControls struct {
	BasePath    string   // e.g. "/hosts"
	Columns     []string // filterable/sortable column labels ("Name" + fields + custom fields)
	Tags        []string // tag names present on this entity type
	Sort        string   // current sort column ("" = none)
	Dir         string   // "asc" or "desc"
	Tag         string   // current tag filter ("" = none)
	FilterField string   // current filter column ("" = none)
	FilterValue string   // current filter substring
}

// Active reports whether any filter or sort is applied, so the template can show
// a "clear" affordance only when it does something.
func (c listControls) Active() bool {
	return c.Sort != "" || c.Tag != "" || (c.FilterField != "" && c.FilterValue != "")
}

// filterSort applies the request's query-param filters and sort to items and
// returns the result plus the control state to render. It is generic across
// every catalog entity: columns come from the resource's own field labels (plus
// its custom fields), so no per-type code is needed. Tag and custom-field values
// are bulk-loaded; standard field values come from the resource's fields()
// accessor.
func (rs resource[T]) filterSort(items []T, req *http.Request, d handlerDeps) ([]T, listControls, error) {
	q := req.URL.Query()
	ctrl := listControls{
		BasePath:    rs.basePath(),
		Sort:        strings.TrimSpace(q.Get("sort")),
		Dir:         strings.TrimSpace(q.Get("dir")),
		Tag:         strings.TrimSpace(q.Get("tag")),
		FilterField: strings.TrimSpace(q.Get("field")),
		FilterValue: strings.TrimSpace(q.Get("value")),
	}
	if ctrl.Dir != "desc" {
		ctrl.Dir = "asc"
	}

	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, rs.id(it))
	}
	cfValues, err := d.customFields.ValuesForEntities(rs.sing, ids)
	if err != nil {
		return nil, ctrl, err
	}

	// Build the display projection (id -> label -> value) and the ordered,
	// de-duplicated column set. Standard labels come from the zero value so the
	// column list is stable even when the list is empty.
	cols := []string{"Name"}
	seen := map[string]bool{"Name": true}
	addCol := func(label string) {
		if label != "" && !seen[label] {
			seen[label] = true
			cols = append(cols, label)
		}
	}
	if rs.fields != nil {
		for _, fr := range rs.fields(rs.newItem) {
			addCol(fr.Label)
		}
	}
	proj := make(map[int64]map[string]string, len(items))
	for _, it := range items {
		id := rs.id(it)
		m := map[string]string{"Name": rs.label(it)}
		if rs.fields != nil {
			for _, fr := range rs.fields(it) {
				m[fr.Label] = fr.Value
			}
		}
		for _, v := range cfValues[id] {
			m[v.Label] = v.Value
			addCol(v.Label)
		}
		proj[id] = m
	}
	ctrl.Columns = cols

	// Available tags for this entity type, plus the per-tag id sets used to
	// filter. Both come from one tags.List() so there is no per-tag query.
	allTags, err := d.tags.List()
	if err != nil {
		return nil, ctrl, err
	}
	var tagNames []string
	tagSeen := map[string]bool{}
	tagged := map[string]map[int64]bool{}
	for _, t := range allTags {
		if t.EntityType != rs.sing {
			continue
		}
		if !tagSeen[t.Name] {
			tagSeen[t.Name] = true
			tagNames = append(tagNames, t.Name)
		}
		if tagged[t.Name] == nil {
			tagged[t.Name] = map[int64]bool{}
		}
		tagged[t.Name][t.EntityID] = true
	}
	sort.Strings(tagNames)
	ctrl.Tags = tagNames

	out := make([]T, 0, len(items))
	for _, it := range items {
		id := rs.id(it)
		if ctrl.Tag != "" && !tagged[ctrl.Tag][id] {
			continue
		}
		if ctrl.FilterField != "" && ctrl.FilterValue != "" {
			if !strings.Contains(strings.ToLower(proj[id][ctrl.FilterField]), strings.ToLower(ctrl.FilterValue)) {
				continue
			}
		}
		out = append(out, it)
	}

	if ctrl.Sort != "" {
		sort.SliceStable(out, func(i, j int) bool {
			c := compareValues(proj[rs.id(out[i])][ctrl.Sort], proj[rs.id(out[j])][ctrl.Sort])
			if ctrl.Dir == "desc" {
				return c > 0
			}
			return c < 0
		})
	}
	return out, ctrl, nil
}

// compareValues orders two cell values: numerically when both parse as numbers
// (so "10" sorts after "9"), otherwise case-insensitively. Returns -1, 0, or 1.
func compareValues(a, b string) int {
	if fa, ea := strconv.ParseFloat(strings.TrimSpace(a), 64); ea == nil {
		if fb, eb := strconv.ParseFloat(strings.TrimSpace(b), 64); eb == nil {
			switch {
			case fa < fb:
				return -1
			case fa > fb:
				return 1
			default:
				return 0
			}
		}
	}
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}
