package web

import (
	"context"
	"database/sql"
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

type fakeXenOrchestraScanner struct {
	resources []discovery.XenOrchestraResource
	err       error
	calls     int
}

func (f *fakeXenOrchestraScanner) Resources(context.Context) ([]discovery.XenOrchestraResource, error) {
	f.calls++
	return f.resources, f.err
}

func xenOrchestraTestServer(t *testing.T, scanner xenOrchestraScanner, enabled, auth bool, sources ...[]DiscoverySource) (http.Handler, *sql.DB) {
	t.Helper()
	db := rbacDB(t)
	cfg := Config{
		Hosts: store.NewHostRepo(db), Services: store.NewServiceRepo(db), Networks: store.NewNetworkRepo(db),
		Domains: store.NewDomainRepo(db), Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db), Accounts: store.NewAccountRepo(db),
		Sites: store.NewSiteRepo(db), Locations: store.NewLocationRepo(db), Racks: store.NewRackRepo(db), Contacts: store.NewContactRepo(db),
		Relationships: store.NewRelationshipRepo(db), Tags: store.NewTagRepo(db), VLANs: store.NewVLANRepo(db), Reservations: store.NewReservationRepo(db),
		DB: db, Logger: testLogger(), Docker: fakeScanner{}, NetScan: fakeNetworkScanner{}, Proxmox: fakeProxmoxScanner{},
		XenOrchestra: scanner, XOOpts: XenOrchestraOptions{Enabled: enabled}, AuthEnabled: auth,
	}
	if len(sources) > 0 {
		cfg.DiscoverySources = sources[0]
	}
	return New(cfg), db
}

func xenOrchestraFixture() []discovery.XenOrchestraResource {
	return []discovery.XenOrchestraResource{
		{ID: "host-a", Type: "host", Name: "xcp-a", Status: "running", CPU: 8, Memory: 16 << 30, IPs: []string{"192.0.2.10"}},
		{ID: "vm-a", Type: "vm", Name: "app-a", HostID: "host-a", Status: "running", CPU: 2, Memory: 2 << 30},
	}
}

func TestXenOrchestraDisabled(t *testing.T) {
	for _, missingClient := range []bool{false, true} {
		scanner := &fakeXenOrchestraScanner{resources: xenOrchestraFixture()}
		var client xenOrchestraScanner = scanner
		if missingClient {
			client = nil
		}
		srv, _ := xenOrchestraTestServer(t, client, missingClient, false)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/discovery/xen-orchestra", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("disabled review: status %d", rec.Code)
		}
		if rec := postForm(t, srv, "/discovery/xen-orchestra/import", nil); rec.Code != http.StatusNotFound {
			t.Fatalf("disabled import: status %d", rec.Code)
		}
		if scanner.calls != 0 {
			t.Fatal("disabled source called scanner")
		}
	}
}

func TestXenOrchestraReviewAndLanding(t *testing.T) {
	scanner := &fakeXenOrchestraScanner{resources: xenOrchestraFixture()}
	srv, _ := xenOrchestraTestServer(t, scanner, true, false)
	for path, texts := range map[string][]string{
		"/discovery":               {"/discovery/xen-orchestra", "XCP-ng / Xen Orchestra"},
		"/discovery/xen-orchestra": {"xcp-a", "app-a", "192.0.2.10", "Import selected", `value="vm-a"`},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		for _, want := range texts {
			if !strings.Contains(rec.Body.String(), want) {
				t.Errorf("%s missing %q", path, want)
			}
		}
	}
	disabled, _ := xenOrchestraTestServer(t, scanner, false, false)
	rec := httptest.NewRecorder()
	disabled.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/discovery", nil))
	if strings.Contains(rec.Body.String(), "/discovery/xen-orchestra") {
		t.Fatal("disabled source appeared on discovery page")
	}
}

func TestXenOrchestraImportIsAdditiveAndIdempotent(t *testing.T) {
	scanner := &fakeXenOrchestraScanner{resources: xenOrchestraFixture()}
	srv, db := xenOrchestraTestServer(t, scanner, true, false)
	form := url.Values{"id": {"vm-a", "host-a", "forged-id"}, "link": {"on"}, "name": {"forged-name"}}
	for i := 0; i < 2; i++ {
		rec := postForm(t, srv, "/discovery/xen-orchestra/import", form)
		if rec.Code != http.StatusOK {
			t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
		}
	}
	hosts, err := store.NewHostRepo(db).List()
	if err != nil || len(hosts) != 2 {
		t.Fatalf("hosts = %#v, err %v", hosts, err)
	}
	byName := make(map[string]domain.Host)
	for _, host := range hosts {
		byName[host.Name] = host
	}
	rels, err := store.NewRelationshipRepo(db).List()
	if err != nil || len(rels) != 1 || rels[0].FromID != byName["app-a"].ID || rels[0].ToID != byName["xcp-a"].ID || rels[0].Kind != "runs on" {
		t.Fatalf("relationships = %#v, err %v", rels, err)
	}
	if scanner.calls != 2 {
		t.Fatalf("imports must requery source, calls = %d", scanner.calls)
	}
}

func TestXenOrchestraImportPreservesExistingHost(t *testing.T) {
	scanner := &fakeXenOrchestraScanner{resources: xenOrchestraFixture()}
	srv, db := xenOrchestraTestServer(t, scanner, true, false)
	hr := store.NewHostRepo(db)
	nodeID, err := hr.Create(domain.Host{Name: "XCP-A", Type: "physical", Notes: "Manual notes", CPU: "Manual CPU", Status: "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, srv, "/discovery/xen-orchestra/import", url.Values{"id": {"host-a", "vm-a"}, "link": {"on"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	node, err := hr.Get(nodeID)
	if err != nil || node.Name != "XCP-A" || node.Notes != "Manual notes" || node.CPU != "Manual CPU" || node.Status != "maintenance" {
		t.Fatalf("existing host changed: %#v, err %v", node, err)
	}
	rels, err := store.NewRelationshipRepo(db).List()
	if err != nil || len(rels) != 1 || rels[0].ToID != nodeID {
		t.Fatalf("existing host link = %#v, err %v", rels, err)
	}
}

func TestXenOrchestraImportSkipsAmbiguousNames(t *testing.T) {
	resources := xenOrchestraFixture()
	resources = append(resources, discovery.XenOrchestraResource{ID: "vm-b", Type: "vm", Name: "APP-A", HostID: "host-a"})
	srv, db := xenOrchestraTestServer(t, &fakeXenOrchestraScanner{resources: resources}, true, false)
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "app-a", Type: "vm", Notes: "Unbound manual record"}); err != nil {
		t.Fatal(err)
	}
	review := httptest.NewRecorder()
	srv.ServeHTTP(review, httptest.NewRequest(http.MethodGet, "/discovery/xen-orchestra", nil))
	if review.Code != http.StatusOK || !strings.Contains(review.Body.String(), "Name or identity conflict") || strings.Contains(review.Body.String(), `value="vm-a"`) {
		t.Fatalf("ambiguous review = %d %s", review.Code, review.Body.String())
	}
	rec := postForm(t, srv, "/discovery/xen-orchestra/import", url.Values{"id": {"host-a", "vm-a", "vm-b"}, "link": {"on"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	hosts, err := store.NewHostRepo(db).List()
	if err != nil || len(hosts) != 2 {
		t.Fatalf("ambiguous hosts imported: %#v, err %v", hosts, err)
	}
}

func TestXenOrchestraImportDoesNotGuessParent(t *testing.T) {
	for _, scenario := range []string{"ambiguous-local-name", "wrong-local-type", "missing-parent", "link-disabled", "pool-container"} {
		t.Run(scenario, func(t *testing.T) {
			resources := xenOrchestraFixture()
			if scenario == "pool-container" {
				resources[1].HostID = "pool-a"
			}
			srv, db := xenOrchestraTestServer(t, &fakeXenOrchestraScanner{resources: resources}, true, false)
			hr := store.NewHostRepo(db)
			types := []string{}
			switch scenario {
			case "ambiguous-local-name":
				types = []string{"physical", "physical"}
			case "wrong-local-type":
				types = []string{"vm"}
			case "link-disabled", "pool-container":
				types = []string{"physical"}
			}
			for _, typ := range types {
				if _, err := hr.Create(domain.Host{Name: "xcp-a", Type: typ}); err != nil {
					t.Fatal(err)
				}
			}
			form := url.Values{"id": {"vm-a"}}
			if scenario != "link-disabled" {
				form.Set("link", "on")
			}
			rec := postForm(t, srv, "/discovery/xen-orchestra/import", form)
			if rec.Code != http.StatusOK {
				t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
			}
			hosts, err := hr.List()
			if err != nil || len(hosts) != len(types)+1 {
				t.Fatalf("hosts = %#v, err %v", hosts, err)
			}
			rels, err := store.NewRelationshipRepo(db).List()
			if err != nil || len(rels) != 0 {
				t.Fatalf("unexpected relationship = %#v, err %v", rels, err)
			}
		})
	}
}

func TestXenOrchestraImportRollsBackOnLinkFailure(t *testing.T) {
	srv, db := xenOrchestraTestServer(t, &fakeXenOrchestraScanner{resources: xenOrchestraFixture()}, true, false)
	if _, err := db.Exec(`CREATE TRIGGER fail_xo_link BEFORE INSERT ON relationships BEGIN SELECT RAISE(ABORT, 'test relationship failure'); END`); err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, srv, "/discovery/xen-orchestra/import", url.Values{"id": {"host-a", "vm-a"}, "link": {"on"}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("import: status %d", rec.Code)
	}
	hosts, err := store.NewHostRepo(db).List()
	if err != nil || len(hosts) != 0 {
		t.Fatalf("partial import persisted: %#v, err %v", hosts, err)
	}
}

func TestXenOrchestraSourceFailureDoesNotImport(t *testing.T) {
	srv, db := xenOrchestraTestServer(t, &fakeXenOrchestraScanner{err: errors.New("source unavailable")}, true, false)
	review := httptest.NewRecorder()
	srv.ServeHTTP(review, httptest.NewRequest(http.MethodGet, "/discovery/xen-orchestra", nil))
	if review.Code != http.StatusOK || !strings.Contains(review.Body.String(), "Could not reach the Xen Orchestra API") {
		t.Fatalf("review failure: %d %s", review.Code, review.Body.String())
	}
	if rec := postForm(t, srv, "/discovery/xen-orchestra/import", url.Values{"id": {"host-a"}}); rec.Code != http.StatusBadGateway {
		t.Fatalf("import failure: status %d", rec.Code)
	}
	hosts, err := store.NewHostRepo(db).List()
	if err != nil || len(hosts) != 0 {
		t.Fatalf("failed source imported data: %#v, err %v", hosts, err)
	}
}

func TestXenOrchestraImportRequiresWriterAndCSRF(t *testing.T) {
	scanner := &fakeXenOrchestraScanner{resources: xenOrchestraFixture()}
	srv, db := xenOrchestraTestServer(t, scanner, true, true)
	viewer := seedUserAndLogin(t, srv, db, "xo-viewer", domain.RoleViewer)
	editor := seedUserAndLogin(t, srv, db, "xo-editor", domain.RoleEditor)
	if code := csrfPost(t, srv, viewer, "/discovery/xen-orchestra/import", "id=host-a"); code != http.StatusForbidden {
		t.Fatalf("viewer import = %d", code)
	}
	badCSRF := httptest.NewRecorder()
	req := withCookie(httptest.NewRequest(http.MethodPost, "/discovery/xen-orchestra/import", strings.NewReader("id=host-a")), editor)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(badCSRF, req)
	if badCSRF.Code != http.StatusForbidden || scanner.calls != 0 {
		t.Fatalf("missing CSRF = %d, scanner calls = %d", badCSRF.Code, scanner.calls)
	}
	if code := csrfPost(t, srv, editor, "/discovery/xen-orchestra/import", "id=host-a"); code != http.StatusOK {
		t.Fatalf("editor import = %d", code)
	}
}
