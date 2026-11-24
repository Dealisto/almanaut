package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestDiscoveryChangesRoutesEnforcePermissionsAndCSRF(t *testing.T) {
	scanner := &replacementScanner{containers: []discovery.Container{{ID: "tracked", Name: "source-name", ComposeProject: "observed-project"}}}
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "podman", Docker: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, true, []DiscoverySource{source})
	service := domain.Service{Name: "manual-name", Kind: "container", Category: "manual-category", Notes: "Keep these notes", URL: "https://example.test"}
	id, err := store.NewServiceRepo(db).Create(service)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: source.Key(), ExternalID: "tracked", EntityType: "service", EntityID: id}); err != nil {
		t.Fatal(err)
	}
	viewer := seedUserAndLogin(t, srv, db, "changes-viewer", domain.RoleViewer)
	editor := seedUserAndLogin(t, srv, db, "changes-editor", domain.RoleEditor)
	path := source.Path() + "/changes"
	read := func(cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		srv.ServeHTTP(r, withCookie(httptest.NewRequest(http.MethodGet, path, nil), cookie))
		return r
	}
	rec := read(viewer)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `name="field"`) {
		t.Fatalf("viewer review: %d", rec.Code)
	}
	rec = read(editor)
	expected := regexp.MustCompile(`name="expected_name" value="([a-f0-9]+)"`).FindStringSubmatch(rec.Body.String())
	if rec.Code != http.StatusOK || len(expected) != 2 {
		t.Fatalf("editor review missing fingerprint: %d %s", rec.Code, rec.Body.String())
	}
	form := url.Values{"external_id": {"tracked"}, "entity_type": {"service"}, "entity_id": {strconv.FormatInt(id, 10)}, "field": {"name"}, "expected_name": {expected[1]}}.Encode()
	calls := scanner.calls
	if code := csrfPost(t, srv, viewer, path, form); code != http.StatusForbidden {
		t.Fatalf("viewer apply: %d", code)
	}
	req := withCookie(httptest.NewRequest(http.MethodPost, path, strings.NewReader(form)), editor)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || scanner.calls != calls {
		t.Fatalf("rejected request reached scan: status=%d calls=%d", rec.Code, scanner.calls)
	}
	if code := csrfPost(t, srv, editor, path, form); code != http.StatusOK {
		t.Fatalf("editor apply: %d", code)
	}
	updated, err := store.NewServiceRepo(db).Get(id)
	if err != nil || updated.Name != "source-name" || updated.Category != service.Category || updated.Notes != service.Notes || updated.URL != service.URL {
		t.Fatalf("wrong selected fields applied: %+v %v", updated, err)
	}
	journal, err := store.NewChangelogRepo(db).ListForEntity("service", id)
	if err != nil || len(journal) != 1 || journal[0].Actor != "changes-editor" || len(journal[0].Changes) != 1 {
		t.Fatalf("wrong audit entry: %+v %v", journal, err)
	}
	// An accepted field update and its audit event are one atomic operation.
	rec = read(editor)
	category := regexp.MustCompile(`name="expected_category" value="([a-f0-9]+)"`).FindStringSubmatch(rec.Body.String())
	if len(category) != 2 {
		t.Fatal("missing category comparison")
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_changes_audit BEFORE INSERT ON changelog BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	form = url.Values{"external_id": {"tracked"}, "entity_type": {"service"}, "entity_id": {strconv.FormatInt(id, 10)}, "field": {"category"}, "expected_category": {category[1]}}.Encode()
	if code := csrfPost(t, srv, editor, path, form); code != http.StatusInternalServerError {
		t.Fatalf("audit failure apply: %d", code)
	}
	updated, err = store.NewServiceRepo(db).Get(id)
	if err != nil || updated.Category != service.Category {
		t.Fatalf("field update survived audit failure: %+v %v", updated, err)
	}
}
