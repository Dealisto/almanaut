package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestDiscoveryReplacementSuggestionsRejectAmbiguity(t *testing.T) {
	old := domain.DiscoveryBinding{Source: "podman:lab", ExternalID: "old", EntityType: "service", EntityID: 1}
	service := domain.Service{ID: 1, Name: "App", Kind: "container"}
	candidate := discovery.Candidate{ID: "new", EntityType: "service", Service: domain.Service{Name: " app ", Kind: "container"}}
	cases := []struct {
		name       string
		candidates []discovery.Candidate
		services   []domain.Service
		bindings   []domain.DiscoveryBinding
		want       int
	}{
		{"unique", []discovery.Candidate{candidate}, []domain.Service{service}, []domain.DiscoveryBinding{old}, 1},
		{"old still observed", []discovery.Candidate{candidate, {ID: "old", EntityType: "service", Service: domain.Service{Name: "other"}}}, []domain.Service{service}, []domain.DiscoveryBinding{old}, 0},
		{"duplicate candidate ID", []discovery.Candidate{candidate, candidate}, []domain.Service{service}, []domain.DiscoveryBinding{old}, 0},
		{"duplicate candidate name", []discovery.Candidate{candidate, {ID: "other", EntityType: "service", Service: candidate.Service}}, []domain.Service{service}, []domain.DiscoveryBinding{old}, 0},
		{"duplicate inventory name", []discovery.Candidate{candidate}, []domain.Service{service, {ID: 2, Name: "App", Kind: "container"}}, []domain.DiscoveryBinding{old, {Source: old.Source, ExternalID: "other-old", EntityType: "service", EntityID: 2}}, 0},
		{"equal name in independent source", []discovery.Candidate{candidate}, []domain.Service{service, {ID: 2, Name: "App", Kind: "container"}}, []domain.DiscoveryBinding{old, {Source: "podman:other", ExternalID: "old", EntityType: "service", EntityID: 2}}, 1},
		{"other source", []discovery.Candidate{candidate}, []domain.Service{service}, []domain.DiscoveryBinding{{Source: "podman:other", ExternalID: "old", EntityType: "service", EntityID: 1}}, 0},
		{"new identity already bound", []discovery.Candidate{candidate}, []domain.Service{service}, []domain.DiscoveryBinding{old, {Source: old.Source, ExternalID: "new", EntityType: "service", EntityID: 2}}, 0},
		{"multiple old identities", []discovery.Candidate{candidate}, []domain.Service{service}, []domain.DiscoveryBinding{old, {Source: old.Source, ExternalID: "old2", EntityType: "service", EntityID: 1}}, 0},
		{"different kind", []discovery.Candidate{candidate}, []domain.Service{{ID: 1, Name: "App", Kind: "native"}}, []domain.DiscoveryBinding{old}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := discoveryRebindOptions(old.Source, tc.candidates, nil, tc.services, tc.bindings)
			if len(got) != tc.want {
				t.Fatalf("suggestions=%+v, want %d", got, tc.want)
			}
		})
	}
	h := domain.Host{ID: 1, Name: "guest", Type: "vm"}
	b := domain.DiscoveryBinding{Source: "proxmox:lab", ExternalID: "old", EntityType: "host", EntityID: 1}
	c := discovery.Candidate{ID: "new", EntityType: "host", Host: domain.Host{Name: "guest", Type: "lxc"}}
	if got := discoveryRebindOptions(b.Source, []discovery.Candidate{c}, []domain.Host{h}, nil, []domain.DiscoveryBinding{b}); len(got) != 0 {
		t.Fatal("host type mismatch offered")
	}
}

type replacementScanner struct {
	containers []discovery.Container
	err        error
	calls      int
}

func (s *replacementScanner) Containers(context.Context) ([]discovery.Container, error) {
	s.calls++
	return s.containers, s.err
}

func TestDiscoveryReplacementPreservesInventoryAndRejectsStaleConfirmations(t *testing.T) {
	db := rbacDB(t)
	sr, hr, br, rr := store.NewServiceRepo(db), store.NewHostRepo(db), store.NewDiscoveryBindingRepo(db), store.NewRelationshipRepo(db)
	id, err := sr.Create(domain.Service{Name: "app", Kind: "container", Notes: "Manual notes", URL: "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	hid, err := hr.Create(domain.Host{Name: "host", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rr.Create(domain.Relationship{FromType: "service", FromID: id, ToType: "host", ToID: hid, Kind: "runs on"})
	if err != nil {
		t.Fatal(err)
	}
	old := domain.DiscoveryBinding{Source: "podman:lab", ExternalID: "old", EntityType: "service", EntityID: id}
	if err = br.Create(old); err != nil {
		t.Fatal(err)
	}
	original, _ := sr.Get(id)
	relationships, _ := rr.List()
	scanner := &replacementScanner{containers: []discovery.Container{{ID: "new", Name: "app", Image: "changed"}}}
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "podman", Docker: scanner}
	handler := replaceDiscoveryBinding(source, db, entityCatalog{})
	form := url.Values{"old_id": {"old"}, "new_id": {"new"}, "entity_type": {"service"}, "entity_id": {strconv.FormatInt(id, 10)}, "confirm": {"replace"}}
	post := func(values url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, source.Path()+"/rebind", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	bad := url.Values{}
	for k, v := range form {
		bad[k] = append([]string(nil), v...)
	}
	bad.Del("confirm")
	if rec := post(bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed status=%d", rec.Code)
	}
	if scanner.calls != 0 {
		t.Fatal("unconfirmed form scanned")
	}
	scanner.containers = append(scanner.containers, discovery.Container{ID: "old", Name: "stopped-replacement-now-running"})
	if rec := post(form); rec.Code != http.StatusConflict {
		t.Fatalf("old identity observed status=%d", rec.Code)
	}
	scanner.containers = scanner.containers[:1]
	scanner.err = errors.New("source unavailable")
	if rec := post(form); rec.Code != http.StatusBadGateway {
		t.Fatalf("scan failure status=%d", rec.Code)
	}
	scanner.err = nil
	bad["confirm"] = []string{"replace"}
	bad["entity_id"] = []string{strconv.FormatInt(id+1, 10)}
	if rec := post(bad); rec.Code != http.StatusConflict {
		t.Fatalf("wrong entity status=%d", rec.Code)
	}
	if rec := post(form); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/services/"+strconv.FormatInt(id, 10) {
		t.Fatalf("replacement status=%d %s", rec.Code, rec.Body.String())
	}
	if rec := post(form); rec.Code != http.StatusConflict {
		t.Fatalf("stale repeat status=%d", rec.Code)
	}
	got, _ := sr.Get(id)
	rels, _ := rr.List()
	bindings, _ := br.List()
	if !reflect.DeepEqual(got, original) || !reflect.DeepEqual(rels, relationships) {
		t.Fatal("replacement changed inventory or relationships")
	}
	want := old
	want.ExternalID = "new"
	if !reflect.DeepEqual(bindings, []domain.DiscoveryBinding{want}) {
		t.Fatalf("bindings=%+v", bindings)
	}
}

func TestDiscoveryReplacementReviewAndPermissions(t *testing.T) {
	scanner := &replacementScanner{containers: []discovery.Container{{ID: "new", Name: "app"}}}
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "podman", Docker: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, true, []DiscoverySource{source})
	id, err := store.NewServiceRepo(db).Create(domain.Service{Name: "app", Kind: "container"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: source.Key(), ExternalID: "old", EntityType: "service", EntityID: id}); err != nil {
		t.Fatal(err)
	}
	viewer := seedUserAndLogin(t, srv, db, "replacement-viewer", domain.RoleViewer)
	editor := seedUserAndLogin(t, srv, db, "replacement-editor", domain.RoleEditor)
	path := source.Path() + "/rebind"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, path+"?id=new", nil), editor))
	if rec.Code != http.StatusOK {
		t.Fatalf("review: %d %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"Previous identity", "Observed replacement", `value="old"`, `value="new"`, `name="confirm"`, `href="/services/` + strconv.FormatInt(id, 10) + `"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("review missing %s", want)
		}
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, path, nil), viewer))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `name="confirm"`) {
		t.Fatal("viewer received replacement action")
	}
	form := url.Values{"old_id": {"old"}, "new_id": {"new"}, "entity_type": {"service"}, "entity_id": {strconv.FormatInt(id, 10)}, "confirm": {"replace"}}.Encode()
	before := scanner.calls
	if code := csrfPost(t, srv, viewer, path, form); code != http.StatusForbidden {
		t.Fatalf("viewer POST: %d", code)
	}
	bad := withCookie(httptest.NewRequest(http.MethodPost, path, strings.NewReader(form)), editor)
	bad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, bad)
	if rec.Code != http.StatusForbidden || scanner.calls != before {
		t.Fatalf("rejected request invoked scanner: %d calls=%d want=%d", rec.Code, scanner.calls, before)
	}
	if code := csrfPost(t, srv, editor, path, form); code != http.StatusSeeOther {
		t.Fatalf("editor POST: %d", code)
	}
}

func TestDiscoveryReplacementRollsBackIfAuditCannotBeRecorded(t *testing.T) {
	db := rbacDB(t)
	id, err := store.NewServiceRepo(db).Create(domain.Service{Name: "app", Kind: "container"})
	if err != nil {
		t.Fatal(err)
	}
	old := domain.DiscoveryBinding{Source: "podman:lab", ExternalID: "old", EntityType: "service", EntityID: id}
	br := store.NewDiscoveryBindingRepo(db)
	if err := br.Create(old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_replacement_audit BEFORE INSERT ON changelog BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "podman", Docker: &replacementScanner{containers: []discovery.Container{{ID: "new", Name: "app"}}}}
	form := url.Values{"old_id": {"old"}, "new_id": {"new"}, "entity_type": {"service"}, "entity_id": {strconv.FormatInt(id, 10)}, "confirm": {"replace"}}
	req := httptest.NewRequest(http.MethodPost, source.Path()+"/rebind", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	replaceDiscoveryBinding(source, db, entityCatalog{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("audit failure: %d", rec.Code)
	}
	got, err := br.List()
	if err != nil || !reflect.DeepEqual(got, []domain.DiscoveryBinding{old}) {
		t.Fatalf("identity survived failed audit: %+v %v", got, err)
	}
}
