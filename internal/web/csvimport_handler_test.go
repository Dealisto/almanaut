package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// uploadCSV posts a multipart form to /import-csv: the entity type, the
// confirmation flag, the CSV file, and the CSRF field/cookie pair the
// double-submit check requires (mirrors uploadImport's pattern for /import).
func uploadCSV(t *testing.T, srv http.Handler, entityType, csvDoc string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("type", entityType); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("confirm", "on"); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", "import.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(csvDoc)); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField(csrfFieldName, csrfTestToken); err != nil {
		t.Fatal(err)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/import-csv", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfTestToken})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// TestImportCSVHandlerHappyPath drives the full HTTP path: multipart upload ->
// router -> importCSV -> tx commit, then reads the row back through a repo
// built on the same db to confirm it actually persisted (not just a 303).
func TestImportCSVHandlerHappyPath(t *testing.T) {
	srv, db := newTestServerDB(t)

	rec := uploadCSV(t, srv, "host", "name,type,ips\nedge,physical,10.0.0.9\n")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("import-csv = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "csv_imported=1") {
		t.Errorf("redirect Location = %q, want it to contain csv_imported=1", loc)
	}

	hosts, err := store.NewHostRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(hosts) != 1 || hosts[0].Name != "edge" {
		t.Fatalf("hosts after import = %+v, want one host named edge", hosts)
	}
}

// TestImportCSVHandlerRecordsPerRowChangelog checks the M1 history payoff for
// CSV import: each created row gets its own "create" changelog entry, not one
// entry for the whole batch.
func TestImportCSVHandlerRecordsPerRowChangelog(t *testing.T) {
	srv, db := newTestServerDB(t)

	rec := uploadCSV(t, srv, "host", "name,type,ips\nedge,physical,10.0.0.9\ncore,physical,10.0.0.10\n")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("import-csv = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}

	events, err := store.NewChangelogRepo(db).ListRecent(10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	var creates int
	for _, e := range events {
		if e.EntityType == "host" && e.Action == domain.ActionCreate {
			creates++
		}
	}
	if creates != 2 {
		t.Fatalf("want 2 host create changelog entries, got %d (events=%+v)", creates, events)
	}
}

// TestImportCSVHandlerRowErrorAbortsBatch checks that a row-level validation
// failure re-renders the page (200, not a redirect) with the row error, and
// that nothing was written — the all-or-nothing guarantee holds through the
// HTTP handler, not just the resource.importCSV unit tests.
func TestImportCSVHandlerRowErrorAbortsBatch(t *testing.T) {
	srv, db := newTestServerDB(t)

	rec := uploadCSV(t, srv, "host", "name,type\n,vm\n")
	if rec.Code != http.StatusOK {
		t.Fatalf("import-csv with bad row = %d, want 200 (re-render); body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "row 2") {
		t.Errorf("body should mention the failing row 2, got: %s", rec.Body.String())
	}

	hosts, err := store.NewHostRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(hosts) != 0 {
		t.Fatalf("batch should have aborted, but %d hosts persisted", len(hosts))
	}
}
