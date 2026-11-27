package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestDetailReturnPreservesCanonicalListControls(t *testing.T) {
	srv, _ := newTestServerDB(t)
	if rec := postForm(t, srv, "/hardware", url.Values{"name": {"edge & core"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create hardware: %d", rec.Code)
	}
	query := url.Values{"q": {"edge & core"}, "sort": {"Name"}, "dir": {"desc"}, "page": {"1"}, "per_page": {"25"}}
	origin := "/hardware?" + query.Encode()
	body := getBody(t, srv, origin)
	match := regexp.MustCompile(`href="(/hardware/1\?[^"\s]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("record link does not carry its originating list")
	}
	detail := html.UnescapeString(match[1])
	u, err := url.Parse(detail)
	if err != nil || u.Query().Get("return_to") != origin {
		t.Fatalf("record link=%q, error=%v", detail, err)
	}
	body = getBody(t, srv, detail)
	back := regexp.MustCompile(`data-list-return href="([^"]+)"`).FindStringSubmatch(body)
	if len(back) != 2 || html.UnescapeString(back[1]) != origin {
		t.Fatalf("detail omitted the originating list controls: %v", back)
	}
	// Canonical ListURL is also used to build mutation URLs: a return query must
	// never become part of an attachment/journal action.
	for _, path := range []string{`action="/hardware/1/attachments"`, `action="/hardware/1/journal"`} {
		if !strings.Contains(body, path) {
			t.Fatalf("canonical action missing: %s", path)
		}
	}
}

func TestDetailReturnRejectsUnrelatedAndUnsafeTargets(t *testing.T) {
	srv, _ := newTestServerDB(t)
	if rec := postForm(t, srv, "/hardware", url.Values{"name": {"edge"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create hardware: %d", rec.Code)
	}
	for _, target := range []string{
		"", "https://example.invalid/hardware?q=secret", "//example.invalid/hardware", "/hosts?q=other",
		"/hardwares?page=2", "/hardware/1", "/hardware?value=%ZZ", "/hardware\\evil", "/hardware/../hosts",
		"javascript:alert(1)", "/%2fexample.invalid/hardware", "/hardware%0a", "https:/hardware",
	} {
		t.Run(target, func(t *testing.T) {
			body := getBody(t, srv, "/hardware/1?"+url.Values{"return_to": {target}}.Encode())
			if !strings.Contains(body, `data-list-return href="/hardware"`) {
				t.Fatalf("unsafe or unrelated target retained: %q", target)
			}
			if !strings.Contains(body, `href="/hardware/1/edit"`) {
				t.Fatalf("unsafe or unrelated target entered Edit link: %q", target)
			}
		})
	}
}

func TestListNavigationRetainsOnlyKnownControls(t *testing.T) {
	for _, base := range []string{"/hosts", "/hardware"} {
		query := url.Values{"q": {"edge & core"}, "tag": {"production"}, "sort": {"Name"}, "dir": {"desc"}, "field": {"OS"}, "value": {"Linux"}, "page": {"2"}, "per_page": {"25"}}
		origin := base + "?" + query.Encode()
		got := inventoryListReturnPath(base, origin+"&action=delete&ids=9&return_to=%2Fhosts#section")
		if got != origin {
			t.Fatalf("return=%q, want %q", got, origin)
		}
	}
}

func TestListRecordURLDoesNotPropagateUnrelatedNavigationState(t *testing.T) {
	for _, path := range []string{
		"/hardware", "/hardware?return_to=%2Fhosts&action=delete", "/hosts?q=other",
		"/hardware/1?return_to=%2Fhardware%3Fq%3Dprevious", "/hardware?value=%ZZ",
	} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if got := listRecordURL(r, "/hardware", 1); got != "/hardware/1" {
			t.Errorf("request %q generated %q", path, got)
		}
	}
}
