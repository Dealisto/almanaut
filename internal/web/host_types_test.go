package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestHostTypesManagementAndReassignment(t *testing.T) {
	srv, db := newTestServerDB(t)
	post := func(path string, values url.Values, status int) string {
		t.Helper()
		rec := postForm(t, srv, path, values)
		if rec.Code != status {
			t.Fatalf("POST %s: %d, %s", path, rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	get := func(path string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d, %s", path, rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	post("/host-types", url.Values{"label": {"Container"}}, http.StatusSeeOther)
	post("/custom-fields", url.Values{"label": {"Asset tag"}, "entity_type": {"host"}, "kind": {"text"}}, http.StatusSeeOther)
	post("/hosts", url.Values{"name": {"app"}, "type": {"container"}, "notes": {"Keep me"}, "cf_asset_tag": {"ABC"}}, http.StatusSeeOther)
	post("/host-types/container", url.Values{"label": {"Application container"}}, http.StatusSeeOther)
	for _, path := range []string{"/host-types", "/hosts", "/hosts/1", "/hosts/new", "/hosts/1/edit"} {
		if body := get(path); !strings.Contains(body, "Application container") {
			t.Fatalf("missing label on %s", path)
		}
	}
	body := post("/host-types/container/delete", nil, http.StatusOK)
	if !strings.Contains(body, "choose a replacement") {
		t.Fatal("missing in-use explanation")
	}
	for _, replacement := range []string{"container", "missing"} {
		post("/host-types/container/delete", url.Values{"replacement": {replacement}}, http.StatusOK)
		host, err := store.NewHostRepo(db).Get(1)
		if err != nil || host.Type != "container" {
			t.Fatalf("invalid replacement changed host: %+v, %v", host, err)
		}
	}
	post("/host-types/container/delete", url.Values{"replacement": {"vm"}}, http.StatusSeeOther)
	host, err := store.NewHostRepo(db).Get(1)
	if err != nil || host.Type != "vm" || host.Notes != "Keep me" {
		t.Fatalf("reassigned host: %+v, %v", host, err)
	}
	values, err := store.NewCustomFieldRepo(db).ListForEntity("host", 1)
	if err != nil || len(values) != 1 || values[0].Value != "ABC" {
		t.Fatalf("custom fields lost: %+v, %v", values, err)
	}
	history, err := store.NewChangelogRepo(db).ListForEntity("host", 1)
	if err != nil || len(history) != 2 || len(history[0].Changes) != 1 || history[0].Changes[0].Field != "type" || history[0].Changes[0].New != "vm" {
		t.Fatalf("missing type history: %+v, %v", history, err)
	}
	if _, err := store.NewHostTypeRepo(db).Get("container"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("type not deleted: %v", err)
	}
	post("/host-types/vm", url.Values{"label": {"Virtual machine"}}, http.StatusSeeOther)
	post("/host-types/vm/delete", url.Values{"replacement": {"physical"}}, http.StatusOK)
	host, _ = store.NewHostRepo(db).Get(1)
	if host.Type != "vm" || host.TypeLabel != "Virtual machine" {
		t.Fatalf("built-in delete reassigned host: %+v", host)
	}
	post("/host-types", url.Values{"name": {"unused"}, "label": {"Unused"}}, http.StatusSeeOther)
	post("/host-types/unused/delete", nil, http.StatusSeeOther)
	post("/host-types/missing", url.Values{"label": {"Missing"}}, http.StatusNotFound)
	post("/host-types/missing/delete", nil, http.StatusNotFound)
	// Invalid edits retain input, and labels are escaped by html/template.
	body = post("/host-types/vm", url.Values{"label": {" "}}, http.StatusOK)
	if !strings.Contains(body, "label is required") {
		t.Fatal("missing label validation")
	}
	post("/host-types/vm", url.Values{"label": {"<script>bad</script>"}}, http.StatusSeeOther)
	body = get("/hosts")
	if strings.Contains(body, "<script>bad</script>") || !strings.Contains(body, "&lt;script&gt;bad&lt;/script&gt;") {
		t.Fatal("label not safely escaped")
	}
}

func TestHostTypesAPIAndCSV(t *testing.T) {
	srv, db := newTestServerDB(t)
	if err := store.NewHostTypeRepo(db).Create(domain.HostType{Name: "container", Label: "Container"}); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		if rec.Code != status {
			t.Fatalf("%s %s = %d: %s", method, path, rec.Code, rec.Body)
		}
		return rec
	}
	rec := request("GET", "/api/host-types", "", http.StatusOK)
	var types []domain.HostType
	if err := json.Unmarshal(rec.Body.Bytes(), &types); err != nil || len(types) != 5 {
		t.Fatalf("catalog=%+v, %v", types, err)
	}
	rec = request("POST", "/api/hosts", `{"name":"app","type":"container"}`, http.StatusCreated)
	if !strings.Contains(rec.Body.String(), `"type":"container"`) || strings.Contains(rec.Body.String(), "TypeLabel") {
		t.Fatalf("unstable API: %s", rec.Body)
	}
	request("PUT", "/api/hosts/1", `{"name":"app","type":"missing"}`, http.StatusBadRequest)
	request("POST", "/api/hosts", `{"name":"app","type":"missing"}`, http.StatusBadRequest)
	request("PUT", "/api/hosts/1", `{"name":"app","type":"container","os":"Linux"}`, http.StatusOK)
	request("PUT", "/api/hosts/999", `{"name":"app","type":"missing"}`, http.StatusNotFound)
	rec = uploadCSV(t, srv, "host", "name,type\ncsv,container\n")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("CSV custom type: %d %s", rec.Code, rec.Body)
	}
	rec = uploadCSV(t, srv, "host", "name,type\nrollback,container\ninvalid,missing\n")
	if !strings.Contains(rec.Body.String(), "unknown host type") {
		t.Fatalf("CSV missing validation: %s", rec.Body)
	}
	hosts, err := store.NewHostRepo(db).List()
	if err != nil || len(hosts) != 2 {
		t.Fatalf("CSV rollback failed: %+v %v", hosts, err)
	}
}

func TestHostTypesReassignmentRollsBackOnFailure(t *testing.T) {
	srv, db := newTestServerDB(t)
	types, hosts := store.NewHostTypeRepo(db), store.NewHostRepo(db)
	if err := types.Create(domain.HostType{Name: "container", Label: "Container"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if _, err := hosts.Create(domain.Host{Name: name, Type: "container"}); err != nil {
			t.Fatal(err)
		}
	}
	// Fail the second host after the first update and its history have run.
	if _, err := db.Exec(`CREATE TRIGGER fail_reassignment BEFORE UPDATE ON hosts WHEN OLD.name='b' BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, srv, "/host-types/container/delete", url.Values{"replacement": {"vm"}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failure=%d, %s", rec.Code, rec.Body)
	}
	items, err := hosts.List()
	if err != nil || len(items) != 2 {
		t.Fatalf("hosts: %+v, %v", items, err)
	}
	for _, h := range items {
		if h.Type != "container" {
			t.Fatalf("partially reassigned host: %+v", h)
		}
	}
	if _, err := types.Get("container"); err != nil {
		t.Fatal(err)
	}
	history, err := store.NewChangelogRepo(db).ListForEntity("host", 1)
	if err != nil || len(history) != 0 {
		t.Fatalf("partial history: %+v, %v", history, err)
	}
}

func TestHostTypesAuthorizationAndCSRF(t *testing.T) {
	db := rbacDB(t)
	srv := newAuthedTestHandler(t, db)
	viewer := seedUserAndLogin(t, srv, db, "viewer-types", domain.RoleViewer)
	editor := seedUserAndLogin(t, srv, db, "editor-types", domain.RoleEditor)
	for _, path := range []string{"/host-types", "/host-types/vm", "/host-types/vm/delete"} {
		if code := csrfPost(t, srv, viewer, path, "label=Forbidden"); code != http.StatusForbidden {
			t.Fatalf("viewer %s = %d", path, code)
		}
	}
	if code := csrfPost(t, srv, editor, "/host-types", "label=Container"); code != http.StatusSeeOther {
		t.Fatalf("editor create=%d", code)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, withCookie(httptest.NewRequest("POST", "/host-types/container/delete", nil), editor))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF=%d", rec.Code)
	}
	for _, scope := range []domain.Scope{domain.ScopeReadOnly, domain.ScopeAgent} {
		raw := mkToken(t, db, "types-"+string(scope), domain.RoleEditor, scope)
		req := httptest.NewRequest("GET", "/api/host-types", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		want := http.StatusOK
		if scope == domain.ScopeAgent {
			want = http.StatusForbidden
		}
		if rec.Code != want {
			t.Fatalf("scope %s catalog=%d: %s", scope, rec.Code, rec.Body)
		}
	}
}

func TestHostTypesDiscoveryOptionsAndImport(t *testing.T) {
	srv := newTestServerFull(t, fakeScanner{}, fakeNetworkScanner{hosts: []discovery.ScannedHost{{IP: "192.168.1.80", Hostname: "edge"}}}, NetDiscoveryOptions{Enabled: true})
	rec := postForm(t, srv, "/host-types", url.Values{"name": {"container"}, "label": {"Container"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d", rec.Code)
	}
	for _, path := range []string{"/hosts/new"} {
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `value="container"`) {
			t.Fatalf("options on %s: %d %s", path, rec.Code, rec.Body)
		}
	}
	rec = postForm(t, srv, "/discovery/network/scan", url.Values{"subnet": {"192.168.1.0/24"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `value="container"`) {
		t.Fatalf("discovery options: %d %s", rec.Code, rec.Body)
	}
	rec = postForm(t, srv, "/discovery/network/import", url.Values{"type": {"container"}, "host": {"192.168.1.80|edge|80"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("import custom type: %d %s", rec.Code, rec.Body)
	}
	rec = postForm(t, srv, "/discovery/network/import", url.Values{"type": {"missing"}, "host": {"192.168.1.81|other|80"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown import type: %d %s", rec.Code, rec.Body)
	}
}

func TestHostTypesAgentPreservesCustomType(t *testing.T) {
	srv, db, token := agentServer(t, domain.ScopeAgent)
	types := store.NewHostTypeRepo(db)
	if err := types.Create(domain.HostType{Name: "container", Label: "Container"}); err != nil {
		t.Fatal(err)
	}
	id, err := store.NewHostRepo(db).Create(domain.Host{Name: "NAS01", Type: "container"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		rec := postReport(t, srv, token, baseReport())
		if rec.Code != http.StatusOK {
			t.Fatalf("agent report: %d %s", rec.Code, rec.Body)
		}
		host, err := store.NewHostRepo(db).Get(id)
		if err != nil || host.Type != "container" {
			t.Fatalf("agent changed custom type: %+v, %v", host, err)
		}
	}
}
