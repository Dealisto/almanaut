package web

import (
	"net/url"
	"strings"
	"testing"
)

// idxOf is the position of sub in s, or -1; used to assert relative ordering.
func idxOf(s, sub string) int { return strings.Index(s, sub) }

func TestListSortByName(t *testing.T) {
	srv, _ := newTestServerDB(t)
	for _, name := range []string{"zebra", "alpha", "mid"} {
		if rec := postForm(t, srv, "/hosts", url.Values{"name": {name}, "type": {"physical"}}); rec.Code != 303 {
			t.Fatalf("POST /hosts %s = %d", name, rec.Code)
		}
	}

	asc := getBody(t, srv, "/hosts?sort=Name&dir=asc")
	if !(idxOf(asc, "alpha") < idxOf(asc, "mid") && idxOf(asc, "mid") < idxOf(asc, "zebra")) {
		t.Errorf("asc order wrong: alpha=%d mid=%d zebra=%d", idxOf(asc, "alpha"), idxOf(asc, "mid"), idxOf(asc, "zebra"))
	}
	desc := getBody(t, srv, "/hosts?sort=Name&dir=desc")
	if !(idxOf(desc, "zebra") < idxOf(desc, "mid") && idxOf(desc, "mid") < idxOf(desc, "alpha")) {
		t.Errorf("desc order wrong: zebra=%d mid=%d alpha=%d", idxOf(desc, "zebra"), idxOf(desc, "mid"), idxOf(desc, "alpha"))
	}
}

func TestListFilterByField(t *testing.T) {
	srv, _ := newTestServerDB(t)
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"web-up"}, "type": {"physical"}, "status": {"running"}}); rec.Code != 303 {
		t.Fatalf("POST = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"web-down"}, "type": {"physical"}, "status": {"stopped"}}); rec.Code != 303 {
		t.Fatalf("POST = %d", rec.Code)
	}
	body := getBody(t, srv, "/hosts?field=Status&value=running")
	if !strings.Contains(body, "web-up") {
		t.Errorf("expected running host to remain:\n%s", body)
	}
	if strings.Contains(body, "web-down") {
		t.Errorf("stopped host should be filtered out:\n%s", body)
	}
}

func TestListFilterByTag(t *testing.T) {
	srv, _ := newTestServerDB(t)
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"tagged"}, "type": {"physical"}}); rec.Code != 303 {
		t.Fatalf("POST = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"untagged"}, "type": {"physical"}}); rec.Code != 303 {
		t.Fatalf("POST = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/tags", url.Values{"entity_type": {"host"}, "entity_id": {"1"}, "tag": {"prod"}}); rec.Code != 303 {
		t.Fatalf("POST /tags = %d", rec.Code)
	}
	body := getBody(t, srv, "/hosts?tag=prod")
	if !strings.Contains(body, "tagged") {
		t.Errorf("expected tagged host:\n%s", body)
	}
	if strings.Contains(body, "untagged") {
		t.Errorf("untagged host should be filtered out:\n%s", body)
	}
	// The tag appears as an option in the shared controls bar.
	if !strings.Contains(body, "list-controls") || !strings.Contains(body, `value="prod"`) {
		t.Errorf("controls bar missing tag option:\n%s", body)
	}
}

func TestListControlsRendered(t *testing.T) {
	srv, _ := newTestServerDB(t)
	body := getBody(t, srv, "/services")
	if !strings.Contains(body, "list-controls") || !strings.Contains(body, `name="sort"`) {
		t.Errorf("services list missing controls bar:\n%s", body)
	}
}

func TestListSearchCombinesFieldsTagsAndSort(t *testing.T) {
	srv := newTestServer(t)
	if rec := postForm(t, srv, "/custom-fields", url.Values{
		"entity_type": {"host"}, "label": {"Asset tag"}, "kind": {"text"},
	}); rec.Code != 303 {
		t.Fatalf("create custom field = %d", rec.Code)
	}
	for _, name := range []string{"zebra", "alpha", "other"} {
		if rec := postForm(t, srv, "/hosts", url.Values{
			"name": {name}, "type": {"physical"}, "status": {"running"},
			"ips": {"10.20.30.40"}, "notes": {"Emergency console"}, "cf_asset_tag": {"ASSET-9000"},
		}); rec.Code != 303 {
			t.Fatalf("create host = %d: %s", rec.Code, rec.Body.String())
		}
	}
	for _, id := range []string{"1", "2"} {
		if rec := postForm(t, srv, "/tags", url.Values{"entity_type": {"host"}, "entity_id": {id}, "tag": {"prod"}}); rec.Code != 303 {
			t.Fatalf("tag host = %d", rec.Code)
		}
	}
	for _, query := range []string{"  ZEBRA  ", "10.20.30", "emergency", "asset-9000"} {
		body := getBody(t, srv, "/hosts?q="+url.QueryEscape(query))
		if !strings.Contains(body, `href="/hosts/1"`) {
			t.Errorf("query %q did not match host", query)
		}
	}
	body := getBody(t, srv, "/hosts?q=asset-9000&tag=prod&field=Status&value=running&sort=Name&dir=asc")
	a, z := idxOf(body, `href="/hosts/2"`), idxOf(body, `href="/hosts/1"`)
	if a < 0 || z < 0 || a >= z || strings.Contains(body, `href="/hosts/3"`) {
		t.Fatal("combined search, tag, field filter and sort returned the wrong rows or order")
	}
	body = getBody(t, srv, "/hosts?q=asset-9000&field=Status&value=stopped")
	if strings.Contains(body, `href="/hosts/1"`) || !strings.Contains(body, "No matching records") {
		t.Fatal("field filter must still apply to a matching text search")
	}
}

func TestCompareValuesNumeric(t *testing.T) {
	if compareValues("9", "10") >= 0 {
		t.Error("numeric compare: 9 should sort before 10")
	}
	if compareValues("Zebra", "alpha") <= 0 {
		t.Error("string compare should be case-insensitive: alpha before Zebra")
	}
}
