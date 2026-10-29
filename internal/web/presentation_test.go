package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestDashboardDoesNotInferLivenessFromInventoryStatus(t *testing.T) {
	srv, db := newTestServerDB(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}, "status": {"running"}})
	body := getBody(t, srv, "/")
	if !strings.Contains(body, "No liveness results yet") || strings.Contains(body, "Hosts down") {
		t.Fatal("a running inventory status must not be presented as a probe result")
	}
	now := time.Now()
	if err := store.NewLivenessRepo(db).Upsert("host", 1, domain.LivenessStatus{
		Status: domain.LivenessDown, CheckedAt: now, ChangedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	body = getBody(t, srv, "/")
	if !strings.Contains(body, "0 up · 1 down · 0 not checked") || !strings.Contains(body, "Hosts down") || !strings.Contains(body, `href="/hosts/1"`) {
		t.Fatal("a failed probe must surface on the dashboard even when inventory status is running")
	}
}

func TestCheckSummaryIncludesUncheckedRecords(t *testing.T) {
	checks := []*domain.LivenessStatus{nil, {Status: domain.LivenessUp}, {Status: domain.LivenessDown}, {Status: "unknown"}}
	if got := checkSummary(checks); got != "1 up · 1 down · 2 not checked" {
		t.Fatalf("checkSummary = %q", got)
	}
}

func TestPortConnectionsCountCablesOnce(t *testing.T) {
	ports := []domain.Port{
		{ID: 1, Name: "Port 1", OwnerName: "switch", PeerPortID: 2},
		{ID: 2, Name: "eno1", OwnerName: "nas", PeerPortID: 1},
		{ID: 3, Name: "unconnected"},
		{ID: 4, Name: "missing peer", PeerPortID: 99},
	}
	got := buildPortConnections(ports, entityCatalog{resources: []mountable{resource[domain.Port]{sing: "port", name: "ports"}}})
	if len(got) != 1 || got[0].From != "switch / Port 1" || got[0].To != "nas / eno1" || got[0].FromURL != "/ports/1" || got[0].ToURL != "/ports/2" {
		t.Fatalf("connections = %+v", got)
	}
}

func TestStaticScriptRevalidation(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") || rec.Body.Len() == 0 {
		t.Fatalf("script response: status=%d type=%q bytes=%d", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("cached script: status=%d bytes=%d", rec.Code, rec.Body.Len())
	}
	css := httptest.NewRecorder()
	srv.ServeHTTP(css, httptest.NewRequest(http.MethodGet, "/static/app.css", nil))
	if css.Header().Get("ETag") == etag {
		t.Fatal("different assets must not share an ETag")
	}
}

func TestFailedYAMLImportKeepsCSVTypesAndInventory(t *testing.T) {
	srv, db := newTestServerDB(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"keep-me"}, "type": {"physical"}})
	for _, input := range []string{"hosts: [", "hosts:\n  - id: 1\n    name: invalid\n    type: impossible\n"} {
		rec := uploadImport(t, srv, input, true)
		body := rec.Body.String()
		if !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `class="disclosure" open`) {
			t.Error("failed YAML import must reveal the error and restore form")
		}
		for _, typ := range domain.EntityTypes {
			if !strings.Contains(body, `value="`+typ+`"`) {
				t.Errorf("CSV record type %q lost after YAML error", typ)
			}
		}
		items, err := store.NewHostRepo(db).List()
		if err != nil || len(items) != 1 || items[0].Name != "keep-me" {
			t.Fatal("failed import changed the existing inventory")
		}
	}
}
