package web

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func assertFormNavigation(t *testing.T, body, action, origin string) {
	t.Helper()
	if index := strings.Index(body, "<main "); index >= 0 {
		body = body[index:]
	}
	if !strings.Contains(body, `action="`+action+`"`) {
		t.Fatalf("canonical form action missing: %s", action)
	}
	for _, pattern := range []string{`<input type="hidden" name="return_to" value="([^"]*)">`, `data-list-return href="([^"]*)"`} {
		match := regexp.MustCompile(pattern).FindStringSubmatch(body)
		if len(match) != 2 || html.UnescapeString(match[1]) != origin {
			t.Fatalf("navigation %s = %v, want %q", pattern, match, origin)
		}
	}
}

func TestEntityFormsPreserveListAcrossCreateEditAndValidation(t *testing.T) {
	for _, base := range []string{"/hosts", "/hardware"} {
		t.Run(base, func(t *testing.T) {
			srv, _ := newTestServerDB(t)
			origin := base + "?" + url.Values{"q": {"edge & core"}, "tag": {"prod"}, "sort": {"Name"}, "dir": {"desc"}, "field": {"Notes"}, "value": {"rack"}, "page": {"2"}, "per_page": {"25"}}.Encode()
			query := "?" + url.Values{"return_to": {origin}}.Encode()
			assertFormNavigation(t, getBody(t, srv, base+"/new"+query), base, origin)
			invalid := url.Values{"name": {""}, "type": {"physical"}, "notes": {"Keep my draft"}, "return_to": {origin}}
			rec := postForm(t, srv, base, invalid)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Keep my draft") {
				t.Fatalf("create validation lost draft: %d", rec.Code)
			}
			assertFormNavigation(t, rec.Body.String(), base, origin)
			valid := url.Values{"name": {"edge & core"}, "type": {"physical"}, "return_to": {origin}}
			rec = postForm(t, srv, base, valid)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != origin {
				t.Fatalf("create returned %d %q", rec.Code, rec.Header().Get("Location"))
			}
			assertFormNavigation(t, getBody(t, srv, base+"/1/edit"+query), base+"/1", origin)
			rec = postForm(t, srv, base+"/1", invalid)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Keep my draft") {
				t.Fatalf("edit validation lost draft: %d", rec.Code)
			}
			assertFormNavigation(t, rec.Body.String(), base+"/1", origin)
			rec = postForm(t, srv, base+"/1", valid)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != origin {
				t.Fatalf("save returned %d %q", rec.Code, rec.Header().Get("Location"))
			}
		})
	}
}

func TestEntityFormNavigationRejectsUnsafeAndQueryOnlyPostTargets(t *testing.T) {
	for _, target := range []string{"", "https://example.invalid/hardware?q=private", "//example.invalid/hardware", "/hosts?q=other", "/hardware/1", "/hardware?value=%ZZ", "/hardware/../hosts"} {
		t.Run(target, func(t *testing.T) {
			srv, _ := newTestServerDB(t)
			query := "?" + url.Values{"return_to": {target}}.Encode()
			assertFormNavigation(t, getBody(t, srv, "/hardware/new"+query), "/hardware", "/hardware")
			for _, path := range []string{"/hardware", "/hardware/1"} {
				// A valid query-string fallback must not revive a missing or rejected
				// body destination, on either successful submission or validation.
				postPath := path + "?return_to=%2Fhardware%3Fq%3Dquery-only"
				rec := postForm(t, srv, postPath, url.Values{"name": {""}, "return_to": {target}})
				if rec.Code != http.StatusOK {
					t.Fatalf("validation status=%d", rec.Code)
				}
				assertFormNavigation(t, rec.Body.String(), path, "/hardware")
				rec = postForm(t, srv, postPath, url.Values{"name": {"edge"}, "return_to": {target}})
				if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/hardware" {
					t.Fatalf("unsafe return=%d %q", rec.Code, rec.Header().Get("Location"))
				}
			}
		})
	}
}

func TestGeneratePortsPreservesValidatedListReturn(t *testing.T) {
	srv, _ := newTestServerDB(t)
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"switch"}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create owner=%d", rec.Code)
	}
	for _, tc := range []struct{ target, want string }{
		{"/ports?page=2&per_page=25", "/ports?page=2&per_page=25"},
		{"https://example.invalid/ports", "/ports"},
		{"/hosts?q=other", "/ports"},
	} {
		rec := postForm(t, srv, "/ports/generate", url.Values{"owner": {"host:1"}, "prefix": {"Browser port "}, "count": {"2"}, "return_to": {tc.target}})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != tc.want {
			t.Fatalf("generate target %q returned %d %q", tc.target, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestEntityFormReturnSanitizesBodyContextBeforeRerender(t *testing.T) {
	srv, _ := newTestServerDB(t)
	const target = "/hardware?page=2&per_page=25&action=delete&ids=9&return_to=%2Fhosts#section"
	const origin = "/hardware?page=2&per_page=25"
	// The body wins over the query, but unrelated action keys, nested return
	// destinations and fragments never enter the next form or redirect.
	const path = "/hardware?return_to=%2Fhardware%3Fq%3Dignored"
	rec := postForm(t, srv, path, url.Values{"name": {""}, "return_to": {target}})
	if rec.Code != http.StatusOK {
		t.Fatalf("validation status=%d", rec.Code)
	}
	assertFormNavigation(t, rec.Body.String(), "/hardware", origin)
	rec = postForm(t, srv, path, url.Values{"name": {"edge"}, "return_to": {target}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != origin {
		t.Fatalf("create returned %d %q", rec.Code, rec.Header().Get("Location"))
	}
}
