package web

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"testing"
)

func TestBulkActionsPreserveListState(t *testing.T) {
	srv, _ := newTestServerDB(t)
	query := url.Values{"q": {"edge & core"}, "tag": {"production"}, "sort": {"Name"}, "dir": {"desc"}, "field": {"OS"}, "value": {"Linux"}, "page": {"2"}, "per_page": {"25"}}
	path := "/hosts?" + query.Encode()
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"edge & core"}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create status=%d", rec.Code)
	}
	displayedPath := "/hosts?q=edge+%26+core&sort=Name&dir=desc&page=1&per_page=25"
	body := getBody(t, srv, displayedPath)
	match := regexp.MustCompile(`(?s)<form id="bulkform".*?name="return_to" value="([^"]*)"`).FindStringSubmatch(body)
	if len(match) != 2 || html.UnescapeString(match[1]) != displayedPath {
		t.Fatalf("bulk form omitted the originating list state: %v", match)
	}
	for _, action := range []string{"tag-add", "tag-remove", "set-field", "delete", "empty-selection"} {
		t.Run(action, func(t *testing.T) {
			srv, _ := newTestServerDB(t)
			if rec := postForm(t, srv, "/hosts", url.Values{"name": {"edge"}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
				t.Fatalf("create status=%d", rec.Code)
			}
			form := url.Values{"action": {action}, "ids": {"1"}, "tag": {"production"}, "field": {"os"}, "value": {"Linux"}, "return_to": {path}}
			if action == "empty-selection" {
				form.Del("ids")
			}
			rec := postForm(t, srv, "/hosts/bulk", form)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path {
				t.Fatalf("status=%d location=%q, want %q", rec.Code, rec.Header().Get("Location"), path)
			}
		})
	}
}

func TestBulkReturnTargetStaysOnCanonicalResource(t *testing.T) {
	srv, _ := newTestServerDB(t)
	for _, target := range []string{
		"https://example.invalid/hardware?q=secret", "//example.invalid/hardware", "/hosts?q=other", "/hardwares?page=2", "/hardware/1", "/hardware?value=%ZZ", "/hardware\\evil", "/hardware/../hosts",
	} {
		t.Run(target, func(t *testing.T) {
			rec := postForm(t, srv, "/hardware/bulk", url.Values{"action": {"delete"}, "return_to": {target}})
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/hardware" {
				t.Fatalf("unsafe/unrelated target redirected to %q (status %d)", rec.Header().Get("Location"), rec.Code)
			}
		})
	}
	rec := postForm(t, srv, "/hardware/bulk", url.Values{"return_to": {"/hardware?page=2&per_page=25&ids=9&action=delete#section"}})
	if rec.Header().Get("Location") != "/hardware?page=2&per_page=25" {
		t.Fatalf("unexpected retained query state: %q", rec.Header().Get("Location"))
	}
}
