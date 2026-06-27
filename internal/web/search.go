package web

import "strings"

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
