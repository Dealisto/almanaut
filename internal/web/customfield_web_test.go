package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var customFieldDeleteActionRE = regexp.MustCompile(`/custom-fields/(\d+)/delete`)

// TestCustomFieldsPageCRUD exercises the /custom-fields admin page end to
// end: define a field for hosts, confirm it shows up on a host's create form
// and detail page, then delete it and confirm it's gone from the admin list.
func TestCustomFieldsPageCRUD(t *testing.T) {
	srv := newTestServer(t)

	getBody := func(path string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	// 1. GET /custom-fields -> 200, contains "Custom fields".
	code, body := getBody("/custom-fields")
	if code != http.StatusOK {
		t.Fatalf("GET /custom-fields = %d, want 200", code)
	}
	if !strings.Contains(body, "Custom fields") {
		t.Errorf("GET /custom-fields body missing %q:\n%s", "Custom fields", body)
	}

	// 2. POST /custom-fields with entity_type=host, label="Asset tag", kind=text -> 303.
	rec := postForm(t, srv, "/custom-fields", url.Values{
		"entity_type": {"host"}, "label": {"Asset tag"}, "kind": {"text"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /custom-fields = %d, want 303, body: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/custom-fields" {
		t.Errorf("redirect Location = %q, want /custom-fields", loc)
	}

	// 3. GET /custom-fields -> contains "Asset tag" and "asset_tag".
	_, body = getBody("/custom-fields")
	if !strings.Contains(body, "Asset tag") {
		t.Errorf("GET /custom-fields body missing %q:\n%s", "Asset tag", body)
	}
	if !strings.Contains(body, "asset_tag") {
		t.Errorf("GET /custom-fields body missing %q:\n%s", "asset_tag", body)
	}

	// 4. Create a host, set cf_asset_tag, and confirm it shows on the detail page.
	// Creating an entity redirects to its list page (not the detail page), so we
	// find the new host's detail link on the list and follow that.
	hostRec := postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}, "cf_asset_tag": {"ABC-1"}})
	if hostRec.Code != http.StatusSeeOther {
		t.Fatalf("POST /hosts = %d, want 303, body: %s", hostRec.Code, hostRec.Body.String())
	}
	_, hostListBody := getBody("/hosts")
	hostMatch := regexp.MustCompile(`/hosts/(\d+)`).FindStringSubmatch(hostListBody)
	if hostMatch == nil {
		t.Fatalf("could not find host detail link on the hosts list:\n%s", hostListBody)
	}
	_, detailBody := getBody("/hosts/" + hostMatch[1])
	if !strings.Contains(detailBody, "Asset tag") {
		t.Errorf("host detail body missing %q:\n%s", "Asset tag", detailBody)
	}
	if !strings.Contains(detailBody, "ABC-1") {
		t.Errorf("host detail body missing %q:\n%s", "ABC-1", detailBody)
	}

	// 5. POST /custom-fields/{defID}/delete -> 303; GET no longer contains "Asset tag".
	match := customFieldDeleteActionRE.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("could not find delete form action for the custom field def in:\n%s", body)
	}
	delRec := postForm(t, srv, "/custom-fields/"+match[1]+"/delete", nil)
	if delRec.Code != http.StatusSeeOther {
		t.Fatalf("POST /custom-fields/{id}/delete = %d, want 303, body: %s", delRec.Code, delRec.Body.String())
	}
	_, body = getBody("/custom-fields")
	if strings.Contains(body, "Asset tag") {
		t.Errorf("GET /custom-fields body still contains %q after delete:\n%s", "Asset tag", body)
	}
}

// TestCustomFieldSearch verifies a custom field VALUE is matched by global search.
func TestCustomFieldSearch(t *testing.T) {
	srv := newTestServer(t)
	// define a host field and create a host carrying a distinctive value
	if rec := postForm(t, srv, "/custom-fields", url.Values{
		"entity_type": {"host"}, "label": {"Asset tag"}, "kind": {"text"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create def = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/hosts", url.Values{
		"name": {"nas"}, "type": {"physical"}, "cf_asset_tag": {"ZZZ-9000"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create host = %d", rec.Code)
	}
	// web search page finds the host by its custom field value
	req := httptest.NewRequest(http.MethodGet, "/search?q=ZZZ-9000", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, "nas") {
		t.Errorf("search body missing host 'nas' matched by cf value:\n%s", body)
	}
	// /api/search finds it too
	req = httptest.NewRequest(http.MethodGet, "/api/search?q=ZZZ-9000", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, `"nas"`) {
		t.Errorf("api search body missing host matched by cf value:\n%s", body)
	}
}
