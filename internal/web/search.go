package web

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Dealisto/almanaut/internal/store"
)

// searchHit is one matching entity on the search results page.
type searchHit struct {
	Label string
	URL   string
}

// searchGroup is the matches for one entity type.
type searchGroup struct {
	Heading string
	Hits    []searchHit
}

// searchPageData backs search.html.
type searchPageData struct {
	Title  string
	Query  string
	Groups []searchGroup
	Total  int
}

// searchEntry is one entity projected into the fields the search handler needs,
// produced uniformly by every resource so the handler carries no per-type code.
type searchEntry struct {
	Type   string   `json:"type"`
	ID     int64    `json:"id"`
	Label  string   `json:"label"`
	Path   string   `json:"path"`
	Fields []string `json:"-"`
}

// matchesQuery reports whether any field contains q, case-insensitively.
func matchesQuery(fields []string, q string) bool {
	lq := strings.ToLower(q)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), lq) {
			return true
		}
	}
	return false
}

// searchEntities renders the global search results for ?q=…: every entity
// whose searchable fields contain the query, plus entities carrying a matching
// tag, grouped by type and linked to their detail pages. It drives every type
// off the resource catalog, so adding an entity needs no change here — only a
// search field list on its resource definition.
func searchEntities(cat entityCatalog, tags *store.TagRepo) http.HandlerFunc {
	key := func(typ string, id int64) string { return fmt.Sprintf("%s:%d", typ, id) }
	return func(w http.ResponseWriter, req *http.Request) {
		q := strings.TrimSpace(req.URL.Query().Get("q"))
		data := searchPageData{Title: "Search", Query: q}
		if q == "" {
			render(w, req, "search.html", data)
			return
		}

		type bucket struct {
			heading string
			hits    []searchHit
		}
		seen := map[string]bool{}      // dedupe by "type:id"
		labelOf := map[string]string{} // "type:id" -> label, to resolve tag hits
		buckets := map[string]*bucket{}
		order := make([]*bucket, 0, len(cat.resources)) // groups in catalog order
		add := func(b *bucket, typ string, id int64, label, path string) {
			k := key(typ, id)
			if seen[k] {
				return
			}
			seen[k] = true
			b.hits = append(b.hits, searchHit{Label: label, URL: path})
		}

		for _, rs := range cat.resources {
			entries, err := rs.searchEntries()
			if err != nil {
				serverError(w, req, err)
				return
			}
			b := &bucket{heading: rs.searchHeading()}
			buckets[rs.singular()] = b
			order = append(order, b)
			for _, e := range entries {
				labelOf[key(e.Type, e.ID)] = e.Label
				if matchesQuery(e.Fields, q) {
					add(b, e.Type, e.ID, e.Label, e.Path)
				}
			}
		}

		// Fold in tag matches: a matching tag pulls its entity into that group.
		tagged, err := tags.Search(q)
		if err != nil {
			serverError(w, req, err)
			return
		}
		for _, tg := range tagged {
			b, ok := buckets[tg.EntityType]
			if !ok {
				continue
			}
			label, ok := labelOf[key(tg.EntityType, tg.EntityID)]
			if !ok {
				continue // orphan tag (entity deleted) — out of scope, see #28
			}
			add(b, tg.EntityType, tg.EntityID, label, cat.path(tg.EntityType, tg.EntityID))
		}

		for _, b := range order {
			if len(b.hits) == 0 {
				continue
			}
			sort.Slice(b.hits, func(i, j int) bool { return b.hits[i].Label < b.hits[j].Label })
			data.Groups = append(data.Groups, searchGroup{Heading: b.heading, Hits: b.hits})
			data.Total += len(b.hits)
		}
		render(w, req, "search.html", data)
	}
}
