package web

import (
	"database/sql"
	"net/http"
	"strings"
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
	Title      string
	Query      string
	Groups     []searchGroup
	Total      int
	Pagination pagination
}

// searchEntry is one entity projected into the fields the search handler needs,
// produced uniformly by every resource so the handler carries no per-type code.
type searchEntry struct {
	Type  string `json:"type"`
	ID    int64  `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}

// searchEntities renders bounded, catalog-ordered search results, including tags.
func searchEntities(cat entityCatalog, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		p, err := parsePagination(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		entries, p, err := searchPage(req, db, cat, p)
		if err != nil {
			serverError(w, req, err)
			return
		}
		data := searchPageData{Title: "Search", Query: strings.TrimSpace(req.URL.Query().Get("q")), Total: p.Total, Pagination: p}
		for _, entry := range entries {
			rs, _ := cat.resource(entry.Type)
			if len(data.Groups) == 0 || data.Groups[len(data.Groups)-1].Heading != rs.searchHeading() {
				data.Groups = append(data.Groups, searchGroup{Heading: rs.searchHeading()})
			}
			group := &data.Groups[len(data.Groups)-1]
			group.Hits = append(group.Hits, searchHit{Label: entry.Label, URL: entry.Path})
		}
		p.headers(w)
		render(w, req, "search.html", data)
	}
}
