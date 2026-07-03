package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// TestChangelogRecordsCreateUpdateDelete drives the real handlers over a temp
// DB and asserts the changelog rows they write atomically alongside the
// entity mutation.
func TestChangelogRecordsCreateUpdateDelete(t *testing.T) {
	srv, db := newTestServerDB(t)
	changelog := store.NewChangelogRepo(db)

	rec := postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}, "status": {"running"}})
	if rec.Code != 303 {
		t.Fatalf("POST /hosts = %d, want 303", rec.Code)
	}

	events, err := changelog.ListForEntity("host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("want 1 event after create, got %d: %+v", len(events), events)
	}
	if events[0].Action != domain.ActionCreate {
		t.Errorf("create event action = %q, want %q", events[0].Action, domain.ActionCreate)
	}

	rec = postForm(t, srv, "/hosts/1", url.Values{"name": {"nas"}, "type": {"physical"}, "status": {"down"}})
	if rec.Code != 303 {
		t.Fatalf("POST /hosts/1 (update) = %d, want 303", rec.Code)
	}

	events, err = changelog.ListForEntity("host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("want create+update = 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Action != domain.ActionUpdate {
		t.Errorf("newest should be update, got %q", events[0].Action)
	}
	var sawStatusChange bool
	for _, c := range events[0].Changes {
		if c.Field == "status" {
			sawStatusChange = true
			if c.Old != "running" || c.New != "down" {
				t.Errorf("status change = %+v, want running -> down", c)
			}
		}
	}
	if !sawStatusChange {
		t.Errorf("update event missing status field change: %+v", events[0].Changes)
	}

	// no-op update (identical values) writes nothing.
	rec = postForm(t, srv, "/hosts/1", url.Values{"name": {"nas"}, "type": {"physical"}, "status": {"down"}})
	if rec.Code != 303 {
		t.Fatalf("POST /hosts/1 (no-op update) = %d, want 303", rec.Code)
	}
	events, err = changelog.ListForEntity("host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("no-op update added a row: %d: %+v", len(events), events)
	}

	rec = postForm(t, srv, "/hosts/1/delete", nil)
	if rec.Code != 303 {
		t.Fatalf("POST /hosts/1/delete = %d, want 303", rec.Code)
	}
	events, err = changelog.ListForEntity("host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Action != domain.ActionDelete {
		t.Fatalf("delete not recorded: %+v", events)
	}
}

// getBody issues a GET and returns the response body, for tests that only
// need to inspect rendered HTML (the package has no reusable GET helper yet).
func getBody(t *testing.T, srv http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, rec.Code)
	}
	return rec.Body.String()
}

// TestHistoryPageListsRecentActivityNewestFirst drives a create then an update
// through the real handlers and asserts the global /history feed renders both,
// newest first.
func TestHistoryPageListsRecentActivityNewestFirst(t *testing.T) {
	srv, _ := newTestServerDB(t)

	rec := postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}, "status": {"running"}})
	if rec.Code != 303 {
		t.Fatalf("POST /hosts = %d, want 303", rec.Code)
	}
	rec = postForm(t, srv, "/hosts/1", url.Values{"name": {"nas"}, "type": {"physical"}, "status": {"down"}})
	if rec.Code != 303 {
		t.Fatalf("POST /hosts/1 (update) = %d, want 303", rec.Code)
	}

	body := getBody(t, srv, "/history")
	if !strings.Contains(body, "nas") {
		t.Errorf("history page missing entity label:\n%s", body)
	}
	// update recorded after create → its row (cl-update) renders before the
	// create row (cl-create). Anchor on the precise action classes rather than
	// bare "update"/"create" substrings, which could match anywhere in markup.
	if strings.Index(body, "cl-update") > strings.Index(body, "cl-create") {
		t.Errorf("history not newest-first")
	}
}
