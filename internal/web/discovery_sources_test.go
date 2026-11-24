package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func assertImportCount(t *testing.T, rec *httptest.ResponseRecorder, name string, want int) {
	t.Helper()
	expected := `data-import-count="` + name + `">` + strconv.Itoa(want) + `</dd>`
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), expected) {
		t.Fatalf("import %s count: status %d, want %s", name, rec.Code, expected)
	}
}

func TestDiscoverySourcesAreIsolated(t *testing.T) {
	scanner := fakeScanner{containers: []discovery.Container{{ID: "same-id", Name: "same-name"}}}
	sources := []DiscoverySource{{ID: "first", Name: "First engine", Kind: "docker", Docker: scanner}, {ID: "second", Name: "Second engine", Kind: "podman", Docker: scanner}}
	srv, db := xenOrchestraTestServer(t, nil, false, false, sources)
	body := getBody(t, srv, "/discovery")
	for _, s := range sources {
		if !strings.Contains(body, s.Name) || !strings.Contains(body, s.Path()) {
			t.Fatalf("source missing: %s", s.Name)
		}
	}
	for _, s := range sources {
		result := postForm(t, srv, s.Path()+"/import", url.Values{"id": {"same-id"}})
		assertImportCount(t, result, "added", 1)
	}
	services, err := store.NewServiceRepo(db).List()
	if err != nil || len(services) != 2 {
		t.Fatalf("source inventories merged: %+v %v", services, err)
	}
	bindings, err := store.NewDiscoveryBindingRepo(db).List()
	if err != nil || len(bindings) != 2 || bindings[0].EntityID == bindings[1].EntityID {
		t.Fatalf("bindings: %+v %v", bindings, err)
	}
	for _, path := range []string{"/discovery/docker", "/discovery/sources/docker/missing"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 404 {
			t.Fatalf("unconfigured source %s: %d", path, rec.Code)
		}
	}
}

func TestDiscoverySourceIdentitySurvivesRenames(t *testing.T) {
	scanner := &fakeXenOrchestraScanner{resources: xenOrchestraFixture()}
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "xen-orchestra", XenOrchestra: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	form := url.Values{"id": {"host-a", "vm-a"}, "link": {"on"}}
	first := postForm(t, srv, source.Path()+"/import", form)
	assertImportCount(t, first, "added", 2)
	assertImportCount(t, first, "linked", 1)
	hr := store.NewHostRepo(db)
	hosts, err := hr.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hosts {
		h.Name = "manual-" + h.Name
		h.Notes = "Manual notes"
		if err := hr.Update(h); err != nil {
			t.Fatal(err)
		}
	}
	scanner.resources[0].Name = "renamed-source-node"
	scanner.resources[1].Name = "renamed-source-guest"
	second := postForm(t, srv, source.Path()+"/import", form)
	assertImportCount(t, second, "added", 0)
	assertImportCount(t, second, "existing", 2)
	assertImportCount(t, second, "linked", 0)
	hosts, err = hr.List()
	if err != nil || len(hosts) != 2 {
		t.Fatalf("rename duplicated hosts: %+v %v", hosts, err)
	}
	for _, h := range hosts {
		if !strings.HasPrefix(h.Name, "manual-") || h.Notes != "Manual notes" {
			t.Fatalf("manual fields overwritten: %+v", h)
		}
	}
	review := getBody(t, srv, source.Path())
	if strings.Contains(review, `name="id"`) {
		t.Fatal("bound resources offered for import after rename")
	}
}

func TestDiscoveryImportSummaryCountsAndAdoption(t *testing.T) {
	scanner := fakeScanner{containers: []discovery.Container{{ID: "existing", Name: "manual"}, {ID: "new", Name: "new-service"}, {ID: "invalid", Name: ""}}}
	source := DiscoverySource{ID: "lab", Name: "Containers", Kind: "docker", Docker: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	if _, err := store.NewServiceRepo(db).Create(domain.Service{Name: "manual", Kind: "container", Notes: "Keep this"}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"id": {"existing", "new", "invalid", "missing", "new"}}
	rec := postForm(t, srv, source.Path()+"/import?added=999", form)
	assertImportCount(t, rec, "added", 1)
	assertImportCount(t, rec, "existing", 1)
	assertImportCount(t, rec, "skipped", 2)
	bindings, err := store.NewDiscoveryBindingRepo(db).List()
	if err != nil || len(bindings) != 2 {
		t.Fatalf("adoption binding missing: %+v %v", bindings, err)
	}
	records, _ := store.NewServiceRepo(db).List()
	for _, r := range records {
		if r.Name == "manual" && r.Notes != "Keep this" {
			t.Fatal("adoption overwrote manual record")
		}
	}
	again := postForm(t, srv, source.Path()+"/import", form)
	assertImportCount(t, again, "added", 0)
	assertImportCount(t, again, "existing", 2)
}

func TestDiscoveryLinksBindUnselectedExistingParent(t *testing.T) {
	scanner := &fakeXenOrchestraScanner{resources: xenOrchestraFixture()}
	source := DiscoverySource{ID: "lab", Name: "Lab", Kind: "xen-orchestra", XenOrchestra: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	hr := store.NewHostRepo(db)
	parent, err := hr.Create(domain.Host{Name: "xcp-a", Type: "physical", Notes: "Manual parent"})
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, srv, source.Path()+"/import", url.Values{"id": {"vm-a"}, "link": {"on"}})
	assertImportCount(t, rec, "added", 1)
	assertImportCount(t, rec, "linked", 1)
	bindings, err := store.NewDiscoveryBindingRepo(db).List()
	if err != nil || len(bindings) != 2 {
		t.Fatalf("parent not bound: %+v %v", bindings, err)
	}
	for _, b := range bindings {
		if b.ExternalID == "host-a" && b.EntityID != parent {
			t.Fatalf("wrong parent: %+v", b)
		}
	}
}

func TestDiscoverySourcesEmptyDisablesLegacyEngines(t *testing.T) {
	srv, _ := xenOrchestraTestServer(t, &fakeXenOrchestraScanner{resources: xenOrchestraFixture()}, true, false, []DiscoverySource{})
	body := getBody(t, srv, "/discovery")
	if strings.Contains(body, "/discovery/docker") || strings.Contains(body, "/discovery/xen-orchestra") {
		t.Fatal("empty source list retained legacy sources")
	}
}

func TestDiscoveryConcurrentImportsAreIdempotent(t *testing.T) {
	scanner := fakeScanner{containers: []discovery.Container{{ID: "same", Name: "same"}}}
	source := DiscoverySource{ID: "lab", Name: "Containers", Kind: "docker", Docker: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := postForm(t, srv, source.Path()+"/import", url.Values{"id": {"same"}})
			if r.Code != 200 {
				t.Errorf("concurrent import: %d", r.Code)
			}
		}()
	}
	wg.Wait()
	services, err := store.NewServiceRepo(db).List()
	if err != nil || len(services) != 1 {
		t.Fatalf("concurrent duplicate: %+v %v", services, err)
	}
	bindings, err := store.NewDiscoveryBindingRepo(db).List()
	if err != nil || len(bindings) != 1 {
		t.Fatalf("concurrent binding: %+v %v", bindings, err)
	}
}

func TestNetworkDiscoveryImportSummary(t *testing.T) {
	srv := newTestServerNet(t, fakeNetworkScanner{}, NetDiscoveryOptions{Enabled: true})
	postForm(t, srv, "/hosts", url.Values{"name": {"manual"}, "type": {"vm"}, "ips": {"192.0.2.1"}})
	rec := postForm(t, srv, "/discovery/network/import", url.Values{"type": {"vm"}, "host": {"192.0.2.1|existing|22", "192.0.2.2|new|80", "broken", "invalid|name|22", "192.0.2.2|new|80"}})
	assertImportCount(t, rec, "added", 1)
	assertImportCount(t, rec, "existing", 1)
	assertImportCount(t, rec, "skipped", 2)
	if !strings.Contains(rec.Body.String(), `href="/discovery/network"`) {
		t.Fatal("network result review link missing")
	}
}

func TestDiscoveryFreshDuplicateNamesUseDistinctIdentities(t *testing.T) {
	resources := xenOrchestraFixture()
	resources = append(resources, discovery.XenOrchestraResource{ID: "vm-b", Type: "vm", Name: "app-a", HostID: "host-a"})
	scanner := &fakeXenOrchestraScanner{resources: resources}
	srv, db := xenOrchestraTestServer(t, scanner, true, false)
	rec := postForm(t, srv, "/discovery/xen-orchestra/import", url.Values{"id": {"host-a", "vm-a", "vm-b"}, "link": {"on"}})
	assertImportCount(t, rec, "added", 3)
	assertImportCount(t, rec, "linked", 2)
	bindings, err := store.NewDiscoveryBindingRepo(db).List()
	if err != nil || len(bindings) != 3 {
		t.Fatalf("distinct identities lost: %+v %v", bindings, err)
	}
}

func TestDiscoveryContainerRecreationRequiresReview(t *testing.T) {
	scanner := fakeScanner{containers: []discovery.Container{{ID: "old", Name: "app"}, {ID: "new", Name: "app"}}}
	source := DiscoverySource{ID: "lab", Name: "Containers", Kind: "docker", Docker: scanner}
	srv, db := xenOrchestraTestServer(t, nil, false, false, []DiscoverySource{source})
	first := postForm(t, srv, source.Path()+"/import", url.Values{"id": {"old"}})
	assertImportCount(t, first, "added", 1)
	second := postForm(t, srv, source.Path()+"/import", url.Values{"id": {"new"}})
	assertImportCount(t, second, "added", 0)
	assertImportCount(t, second, "skipped", 1)
	services, err := store.NewServiceRepo(db).List()
	if err != nil || len(services) != 1 {
		t.Fatalf("recreated container duplicated: %+v %v", services, err)
	}
}
