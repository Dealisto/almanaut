package web

import (
	"fmt"
	"net/http"
	"net/url"
)

// inventoryListReturnPath retains only list controls on the canonical resource
// route. Unrelated paths and external destinations fall back to the plain list.
func inventoryListReturnPath(base, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Path != base {
		return base
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return base
	}
	retained := make(url.Values)
	for _, key := range []string{"q", "tag", "sort", "dir", "field", "value", "page", "per_page"} {
		if query.Has(key) {
			retained.Set(key, query.Get(key))
		}
	}
	if len(retained) == 0 {
		return base
	}
	return base + "?" + retained.Encode()
}

// listRecordURL carries list state in the link itself, so opening a record in
// another tab and navigation without JavaScript preserve the same return path.
func listRecordURL(r *http.Request, base string, id int64) string {
	record := fmt.Sprintf("%s/%d", base, id)
	origin := inventoryListReturnPath(base, r.URL.RequestURI())
	if origin == base {
		return record
	}
	return record + "?" + url.Values{"return_to": {origin}}.Encode()
}

// listFormURL carries the list's state either directly from a list page or
// through a record's explicit return link. The target is a canonical form path
// supplied by the catalog/templates, never an arbitrary request destination.
func listFormURL(r *http.Request, base, target string) string {
	origin := inventoryListReturnPath(base, r.URL.Query().Get("return_to"))
	if r.URL.Path == base {
		origin = inventoryListReturnPath(base, r.URL.RequestURI())
	}
	if origin == base {
		return target
	}
	return target + "?" + url.Values{"return_to": {origin}}.Encode()
}

// formListReturnPath uses the submitted body on mutations, including validation
// errors. A query-string value must not override or fill in a missing body field.
func formListReturnPath(r *http.Request, base string) string {
	if r.Method == http.MethodGet {
		return inventoryListReturnPath(base, r.URL.Query().Get("return_to"))
	}
	return inventoryListReturnPath(base, r.PostFormValue("return_to"))
}
