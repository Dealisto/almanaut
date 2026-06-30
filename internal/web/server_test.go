package web

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/store"
)

type fakeScanner struct {
	containers []discovery.Container
	err        error
}

func (f fakeScanner) Containers(ctx context.Context) ([]discovery.Container, error) {
	return f.containers, f.err
}

type fakeNetworkScanner struct {
	hosts []discovery.ScannedHost
	err   error
}

func (f fakeNetworkScanner) Scan(ctx context.Context, cidr string, ports []int) ([]discovery.ScannedHost, error) {
	return f.hosts, f.err
}

func newTestServer(t *testing.T) http.Handler {
	return newTestServerFull(t, fakeScanner{}, fakeNetworkScanner{}, NetDiscoveryOptions{})
}

func newTestServerWithScanner(t *testing.T, scanner dockerScanner) http.Handler {
	return newTestServerFull(t, scanner, fakeNetworkScanner{}, NetDiscoveryOptions{})
}

func newTestServerNet(t *testing.T, netscan networkScanner, opts NetDiscoveryOptions) http.Handler {
	return newTestServerFull(t, fakeScanner{}, netscan, opts)
}

func newTestServerFull(t *testing.T, docker dockerScanner, netscan networkScanner, opts NetDiscoveryOptions) http.Handler {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return New(Config{
		Hosts: store.NewHostRepo(db), Services: store.NewServiceRepo(db), Networks: store.NewNetworkRepo(db),
		Domains: store.NewDomainRepo(db), Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db), Accounts: store.NewAccountRepo(db),
		Relationships: store.NewRelationshipRepo(db), Tags: store.NewTagRepo(db), DB: db,
		Logger: log.New(io.Discard, "", 0),
		Docker: docker, NetScan: netscan, NetOpts: opts, Proxmox: fakeProxmoxScanner{}, PVEOpts: ProxmoxOptions{},
	})
}

type fakeProxmoxScanner struct {
	res []discovery.ProxmoxResource
	err error
}

func (f fakeProxmoxScanner) Resources(ctx context.Context) ([]discovery.ProxmoxResource, error) {
	return f.res, f.err
}

func newTestServerProxmox(t *testing.T, pve proxmoxScanner, opts ProxmoxOptions) http.Handler {
	t.Helper()
	srv, _, _ := newTestServerProxmoxRepos(t, pve, opts)
	return srv
}

// newTestServerProxmoxRepos is like newTestServerProxmox but also returns the
// host and relationship repos so a test can assert persisted state directly.
func newTestServerProxmoxRepos(t *testing.T, pve proxmoxScanner, opts ProxmoxOptions) (http.Handler, *store.HostRepo, *store.RelationshipRepo) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	hosts := store.NewHostRepo(db)
	rels := store.NewRelationshipRepo(db)
	srv := New(Config{
		Hosts: hosts, Services: store.NewServiceRepo(db), Networks: store.NewNetworkRepo(db),
		Domains: store.NewDomainRepo(db), Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db), Accounts: store.NewAccountRepo(db),
		Relationships: rels, Tags: store.NewTagRepo(db), DB: db,
		Logger: log.New(io.Discard, "", 0),
		Docker: fakeScanner{}, NetScan: fakeNetworkScanner{}, NetOpts: NetDiscoveryOptions{}, Proxmox: pve, PVEOpts: opts,
	})
	return srv, hosts, rels
}

func TestProxmoxDisabledIs404(t *testing.T) {
	srv := newTestServerProxmox(t, fakeProxmoxScanner{}, ProxmoxOptions{})
	req := httptest.NewRequest(http.MethodGet, "/discovery/proxmox", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled GET /discovery/proxmox = %d, want 404", rec.Code)
	}
}

func TestProxmoxImportDisabledIs404(t *testing.T) {
	srv := newTestServerProxmox(t, fakeProxmoxScanner{}, ProxmoxOptions{})
	req := httptest.NewRequest(http.MethodPost, "/discovery/proxmox/import", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled POST /discovery/proxmox/import = %d, want 404", rec.Code)
	}
}

func TestProxmoxReviewShowsRows(t *testing.T) {
	pve := fakeProxmoxScanner{res: []discovery.ProxmoxResource{
		{Type: "node", Node: "pve", Status: "online", ID: "node/pve", MaxCPU: 8},
		{Type: "qemu", Node: "pve", Name: "web", Status: "running", ID: "qemu/100", MaxCPU: 4},
	}}
	srv := newTestServerProxmox(t, pve, ProxmoxOptions{Enabled: true})
	req := httptest.NewRequest(http.MethodGet, "/discovery/proxmox", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /discovery/proxmox = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "pve") || !strings.Contains(body, "web") {
		t.Errorf("review body missing rows: %s", body)
	}
	if !strings.Contains(body, "qemu/100") {
		t.Errorf("review body missing import key qemu/100")
	}
}

func TestProxmoxImportCreatesHostsAndLinks(t *testing.T) {
	pve := fakeProxmoxScanner{res: []discovery.ProxmoxResource{
		{Type: "node", Node: "pve", Status: "online", ID: "node/pve", MaxCPU: 8},
		{Type: "qemu", Node: "pve", Name: "web", Status: "running", ID: "qemu/100", MaxCPU: 4},
	}}
	srv := newTestServerProxmox(t, pve, ProxmoxOptions{Enabled: true})

	form := url.Values{}
	form.Add("id", "node/pve")
	form.Add("id", "qemu/100")
	form.Set("link", "on")
	req := httptest.NewRequest(http.MethodPost, "/discovery/proxmox/import", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("import = %d, want 303", rec.Code)
	}

	// Verify the hosts page now lists both, and the relationships page shows the link.
	getBody := func(path string) string {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w.Body.String()
	}
	hosts := getBody("/hosts")
	if !strings.Contains(hosts, "pve") || !strings.Contains(hosts, "web") {
		t.Errorf("/hosts missing imported hosts: %s", hosts)
	}
	if rels := getBody("/relationships"); !strings.Contains(rels, "runs on") {
		t.Errorf("/relationships missing runs-on link: %s", rels)
	}
}

// Two guests sharing a name must each get their own runs-on edge: linking is
// keyed on the unique Proxmox resource id, not the (colliding) host name.
func TestProxmoxImportLinksDuplicateGuestNames(t *testing.T) {
	pve := fakeProxmoxScanner{res: []discovery.ProxmoxResource{
		{Type: "node", Node: "pve", Status: "online", ID: "node/pve", MaxCPU: 8},
		{Type: "qemu", Node: "pve", Name: "web", Status: "running", ID: "qemu/100", MaxCPU: 2},
		{Type: "qemu", Node: "pve", Name: "web", Status: "running", ID: "qemu/101", MaxCPU: 2},
	}}
	srv, hostRepo, relRepo := newTestServerProxmoxRepos(t, pve, ProxmoxOptions{Enabled: true})

	form := url.Values{}
	form.Add("id", "node/pve")
	form.Add("id", "qemu/100")
	form.Add("id", "qemu/101")
	form.Set("link", "on")
	req := httptest.NewRequest(http.MethodPost, "/discovery/proxmox/import", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("import = %d, want 303", rec.Code)
	}

	allHosts, err := hostRepo.List()
	if err != nil {
		t.Fatalf("List hosts: %v", err)
	}
	if len(allHosts) != 3 {
		t.Fatalf("imported %d hosts, want 3 (node + 2 same-named guests)", len(allHosts))
	}
	var nodeID int64
	guestIDs := map[int64]bool{}
	for _, h := range allHosts {
		if h.Type == "physical" {
			nodeID = h.ID
		} else {
			guestIDs[h.ID] = true
		}
	}
	if len(guestIDs) != 2 {
		t.Fatalf("got %d distinct guests, want 2", len(guestIDs))
	}

	allRels, err := relRepo.List()
	if err != nil {
		t.Fatalf("List relationships: %v", err)
	}
	linked := map[int64]bool{}
	for _, r := range allRels {
		if r.Kind != "runs on" {
			continue
		}
		if r.FromType != "host" || r.ToType != "host" || r.ToID != nodeID {
			t.Errorf("unexpected edge %+v (want guest -> node %d)", r, nodeID)
			continue
		}
		if linked[r.FromID] {
			t.Errorf("duplicate runs-on edge for guest %d", r.FromID)
		}
		linked[r.FromID] = true
	}
	if len(linked) != 2 {
		t.Fatalf("got %d linked guests, want both guests linked exactly once", len(linked))
	}
	for gid := range guestIDs {
		if !linked[gid] {
			t.Errorf("guest %d has no runs-on edge to its node", gid)
		}
	}
}

func TestNetworkDiscoveryDisabledIs404(t *testing.T) {
	srv := newTestServer(t) // network disabled by default
	req := httptest.NewRequest(http.MethodGet, "/discovery/network", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled GET /discovery/network = %d, want 404", rec.Code)
	}
	// Landing must not advertise the network option when disabled.
	req = httptest.NewRequest(http.MethodGet, "/discovery", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "/discovery/network") {
		t.Error("landing should not link to network scan when disabled")
	}
}

func TestNetworkDiscoveryFormRendersWhenEnabled(t *testing.T) {
	srv := newTestServerNet(t, fakeNetworkScanner{}, NetDiscoveryOptions{Enabled: true, DefaultSubnet: "192.168.1.0/24"})
	req := httptest.NewRequest(http.MethodGet, "/discovery/network", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /discovery/network = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `value="192.168.1.0/24"`) {
		t.Error("scan form should pre-fill the default subnet")
	}
	// Landing should advertise the option when enabled.
	req = httptest.NewRequest(http.MethodGet, "/discovery", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "/discovery/network") {
		t.Error("landing should link to network scan when enabled")
	}
}

func TestNetworkDiscoveryScanReview(t *testing.T) {
	scanner := fakeNetworkScanner{hosts: []discovery.ScannedHost{
		{IP: "192.168.1.50", Hostname: "nas.lan", OpenPorts: []int{80, 443}},
		{IP: "192.168.1.51", OpenPorts: []int{22}},
	}}
	srv := newTestServerNet(t, scanner, NetDiscoveryOptions{Enabled: true})
	// Seed a host owning .51 so it shows as already tracked.
	postForm(t, srv, "/hosts", url.Values{"name": {"box"}, "type": {"vm"}, "ips": {"192.168.1.51"}})

	rec := postForm(t, srv, "/discovery/network/scan", url.Values{"subnet": {"192.168.1.0/24"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST scan = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "nas.lan") {
		t.Error("review missing discovered host")
	}
	if !strings.Contains(body, `value="192.168.1.50|nas.lan|80, 443"`) {
		t.Error("review missing import checkbox value for the new host")
	}
	if !strings.Contains(body, "already tracked") {
		t.Error(".51 should be marked already tracked")
	}
	if !strings.Contains(body, "physical") { // the Type selector
		t.Error("review should include the host-type selector")
	}
}

func TestNetworkDiscoveryScanError(t *testing.T) {
	srv := newTestServerNet(t, fakeNetworkScanner{err: errors.New("boom")}, NetDiscoveryOptions{Enabled: true})
	rec := postForm(t, srv, "/discovery/network/scan", url.Values{"subnet": {"192.168.1.0/24"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("scan error = %d, want 200 banner", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Scan failed") {
		t.Error("expected a friendly scan-error banner")
	}
}

func TestDiscoveryDockerScan(t *testing.T) {
	scanner := fakeScanner{containers: []discovery.Container{
		{ID: "c1", Name: "jellyfin", Ports: []discovery.Port{{Public: 8096, Private: 8096, Proto: "tcp"}}},
		{ID: "c2", Name: "existing-svc"},
	}}
	srv := newTestServerWithScanner(t, scanner)
	postForm(t, srv, "/services", url.Values{"name": {"existing-svc"}, "kind": {"container"}})
	postForm(t, srv, "/hosts", url.Values{"name": {"proxmox"}, "type": {"physical"}})

	req := httptest.NewRequest(http.MethodGet, "/discovery/docker", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /discovery/docker = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "jellyfin") {
		t.Error("scan page missing new container")
	}
	if !strings.Contains(body, `value="c1"`) {
		t.Error("scan page missing checkbox for new container")
	}
	if !strings.Contains(body, "already tracked") {
		t.Error("existing-svc should be marked already tracked")
	}
	if !strings.Contains(body, "proxmox") {
		t.Error("host dropdown should list the host")
	}
}

func TestDiscoveryDockerScanSocketError(t *testing.T) {
	srv := newTestServerWithScanner(t, fakeScanner{err: errors.New("boom")})
	req := httptest.NewRequest(http.MethodGet, "/discovery/docker", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /discovery/docker (socket error) = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Could not reach the Docker socket") {
		t.Error("expected a friendly socket-error banner, not a 500")
	}
}

func TestDiscoveryLanding(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/discovery", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /discovery = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/discovery/docker") {
		t.Error("landing page should link to Docker discovery")
	}
}

func TestCreateAndListHost(t *testing.T) {
	srv := newTestServer(t)

	// Create via form POST
	form := url.Values{"name": {"web01"}, "type": {"vm"}, "os": {"Debian 12"}, "ips": {"10.0.0.5"}}
	req := httptest.NewRequest(http.MethodPost, "/hosts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /hosts status = %d, want 303", rec.Code)
	}

	// List shows the new host
	req = httptest.NewRequest(http.MethodGet, "/hosts", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /hosts status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "web01") {
		t.Errorf("GET /hosts body does not contain created host")
	}
}

func TestCreateHostInvalidShowsError(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"name": {""}, "type": {"vm"}}
	req := httptest.NewRequest(http.MethodPost, "/hosts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid POST status = %d, want 200 (re-render with error)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "name is required") {
		t.Errorf("invalid POST body missing validation error")
	}
}

func TestEditAndUpdateHost(t *testing.T) {
	srv := newTestServer(t)

	// Seed one host (gets id 1 in a fresh DB).
	create := url.Values{
		"name": {"web01"}, "type": {"vm"}, "ips": {"10.0.0.5"},
		"cpu": {"4 cores"}, "ram": {"16GB"}, "disk": {"500GB"},
	}
	req := httptest.NewRequest(http.MethodPost, "/hosts", strings.NewReader(create.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(httptest.NewRecorder(), req)

	// Edit form is prefilled with the existing values, including the spec fields.
	req = httptest.NewRequest(http.MethodGet, "/hosts/1/edit", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /hosts/1/edit = %d, want 200", rec.Code)
	}
	editBody := rec.Body.String()
	if !strings.Contains(editBody, "web01") {
		t.Error("edit form not prefilled with existing host")
	}
	if !strings.Contains(editBody, "16GB") {
		t.Error("edit form not prefilled with CPU/RAM/Disk spec values")
	}

	// Update changes the values, including a spec field.
	upd := url.Values{
		"name": {"web99"}, "type": {"lxc"}, "ips": {"10.0.0.6"},
		"cpu": {"8 cores"}, "ram": {"32GB"}, "disk": {"1TB"},
	}
	req = httptest.NewRequest(http.MethodPost, "/hosts/1", strings.NewReader(upd.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /hosts/1 = %d, want 303", rec.Code)
	}

	// The edit form reflects the updated spec value (proving the round-trip).
	req = httptest.NewRequest(http.MethodGet, "/hosts/1/edit", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	editBody = rec.Body.String()
	if !strings.Contains(editBody, "32GB") || strings.Contains(editBody, "16GB") {
		t.Errorf("edit form did not reflect updated spec value")
	}

	// List reflects the update.
	req = httptest.NewRequest(http.MethodGet, "/hosts", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "web99") || strings.Contains(body, "web01") {
		t.Errorf("list did not reflect update")
	}
}

func TestPagesUseSharedLayout(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/hosts", "/hosts/new"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Almanaut") {
			t.Errorf("GET %s: missing layout brand 'Almanaut'", path)
		}
		if !strings.Contains(body, "<style") {
			t.Errorf("GET %s: missing embedded stylesheet", path)
		}
		if !strings.Contains(body, "prefers-color-scheme") {
			t.Errorf("GET %s: missing dark-mode CSS", path)
		}
	}
}

func TestCreateAndListService(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{"name": {"jellyfin"}, "kind": {"container"}, "url": {"http://10.0.0.20:8096"}, "ports": {"8096"}}
	req := httptest.NewRequest(http.MethodPost, "/services", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /services = %d, want 303", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/services", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /services = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "jellyfin") || !strings.Contains(body, "Almanaut") {
		t.Errorf("GET /services missing service or layout")
	}
}

func TestCreateServiceInvalidShowsError(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"name": {""}, "kind": {"container"}}
	req := httptest.NewRequest(http.MethodPost, "/services", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid POST /services = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "name is required") {
		t.Error("invalid POST /services missing validation error")
	}
}

func TestCreateAndListNetwork(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{"name": {"lan"}, "cidr": {"10.0.0.0/24"}, "gateway": {"10.0.0.1"}}
	req := httptest.NewRequest(http.MethodPost, "/networks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /networks = %d, want 303", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/networks", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "10.0.0.0/24") {
		t.Error("GET /networks missing created network")
	}
}

func TestCreateNetworkInvalidShowsError(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"name": {"lan"}, "cidr": {"not-a-cidr"}}
	req := httptest.NewRequest(http.MethodPost, "/networks", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid POST /networks = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid CIDR") {
		t.Error("invalid POST /networks missing CIDR validation error")
	}
}

func TestCreateAndListDomain(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{"fqdn": {"jellyfin.example.com"}, "provider": {"Cloudflare"}}
	req := httptest.NewRequest(http.MethodPost, "/domains", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /domains = %d, want 303", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/domains", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "jellyfin.example.com") {
		t.Errorf("GET /domains missing created domain (code %d)", rec.Code)
	}
}

func TestCreateDomainInvalidShowsError(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"fqdn": {"localhost"}}
	req := httptest.NewRequest(http.MethodPost, "/domains", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid POST /domains = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid FQDN") {
		t.Error("invalid POST /domains missing FQDN validation error")
	}
}

func TestDomainDetailPage(t *testing.T) {
	srv := newTestServer(t)
	postForm(t, srv, "/domains", url.Values{
		"fqdn": {"example.com"}, "provider": {"cloudflare"},
		"notes": {"renews **yearly**"},
	})

	req := httptest.NewRequest(http.MethodGet, "/domains/1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /domains/1 = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "example.com") {
		t.Error("detail page missing FQDN")
	}
	if !strings.Contains(body, "cloudflare") {
		t.Error("detail page missing provider")
	}
	if !strings.Contains(body, "<strong>yearly</strong>") {
		t.Error("notes not rendered as Markdown")
	}
}

func TestCreateAndListCertificate(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{"subject": {"*.example.com"}, "issuer": {"Let's Encrypt"}, "expires_on": {"2027-01-15"}, "auto_renew": {"on"}}
	req := httptest.NewRequest(http.MethodPost, "/certificates", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /certificates = %d, want 303", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/certificates", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "*.example.com") || !strings.Contains(body, "2027-01-15") {
		t.Error("GET /certificates missing created certificate")
	}
}

func TestCreateCertificateInvalidShowsError(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"subject": {"x"}, "expires_on": {"nope"}}
	req := httptest.NewRequest(http.MethodPost, "/certificates", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid POST /certificates = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "expiry date must be") {
		t.Error("invalid POST /certificates missing date validation error")
	}
}

func TestCertificateDetailPage(t *testing.T) {
	srv := newTestServer(t)
	postForm(t, srv, "/certificates", url.Values{
		"subject": {"example.com"}, "issuer": {"Let's Encrypt"},
		"expires_on": {"2027-01-01"}, "auto_renew": {"on"},
		"notes": {"managed by **certbot**"},
	})

	req := httptest.NewRequest(http.MethodGet, "/certificates/1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /certificates/1 = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Let&#39;s Encrypt") && !strings.Contains(body, "Let's Encrypt") {
		t.Error("detail page missing issuer")
	}
	if !strings.Contains(body, "2027-01-01") {
		t.Error("detail page missing expiry date")
	}
	if !strings.Contains(body, "yes") {
		t.Error("detail page missing auto-renew flag")
	}
	if !strings.Contains(body, "<strong>certbot</strong>") {
		t.Error("notes not rendered as Markdown")
	}
}

func TestCreateAndListBackup(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{"source": {"nextcloud-data"}, "destination": {"Backblaze B2"}, "frequency": {"daily"}, "last_run": {"2026-06-20"}}
	req := httptest.NewRequest(http.MethodPost, "/backups", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /backups = %d, want 303", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/backups", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "nextcloud-data") {
		t.Error("GET /backups missing created backup")
	}
}

func TestCreateBackupInvalidShowsError(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"source": {"x"}, "last_run": {"nope"}}
	req := httptest.NewRequest(http.MethodPost, "/backups", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid POST /backups = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "last run must be") {
		t.Error("invalid POST /backups missing date validation error")
	}
}

func TestBackupDetailPage(t *testing.T) {
	srv := newTestServer(t)
	postForm(t, srv, "/backups", url.Values{
		"source": {"nas"}, "destination": {"b2"}, "frequency": {"daily"},
		"last_run": {"2026-06-01"}, "notes": {"verify **monthly**"},
	})

	req := httptest.NewRequest(http.MethodGet, "/backups/1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /backups/1 = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Backup: nas") {
		t.Error("detail page missing backup heading")
	}
	if !strings.Contains(body, "daily") {
		t.Error("detail page missing frequency")
	}
	if !strings.Contains(body, "<strong>monthly</strong>") {
		t.Error("notes not rendered as Markdown")
	}
}

func TestTagsOverview(t *testing.T) {
	srv := newTestServer(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"proxmox"}, "type": {"physical"}})
	postForm(t, srv, "/tags", url.Values{"entity_type": {"host"}, "entity_id": {"1"}, "tag": {"#Critical"}})

	// overview lists the tag with its count
	req := httptest.NewRequest(http.MethodGet, "/tags", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tags = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "#critical") {
		t.Errorf("overview missing tag: %q", body)
	}
	if body := rec.Body.String(); !strings.Contains(body, "/tags?name=critical") {
		t.Errorf("overview missing drilldown link: %q", body)
	}

	// drilling into a tag lists the tagged entity, linked to its detail page
	req = httptest.NewRequest(http.MethodGet, "/tags?name=critical", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tags?name=critical = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/hosts/1") {
		t.Error("tag drilldown missing link to tagged entity")
	}
	if !strings.Contains(body, "host: proxmox") {
		t.Error("tag drilldown missing entity label")
	}

	// a name query that normalizes to empty (e.g. "#") must show the drilldown's
	// empty state, NOT silently fall back to the full tag cloud.
	req = httptest.NewRequest(http.MethodGet, "/tags?name=%23", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tags?name=%%23 = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "#critical") {
		t.Errorf("punctuation-only name should not show the tag cloud: %q", body)
	}
}

func postForm(t *testing.T, srv http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func getPageBody(t *testing.T, srv http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, rec.Code)
	}
	return rec.Body.String()
}

func TestNetworkDetailShowsIPAM(t *testing.T) {
	srv := newTestServer(t)

	// Create a network and two hosts, one of which is inside the subnet.
	postForm(t, srv, "/networks", url.Values{
		"name": {"lan"}, "cidr": {"192.168.1.0/24"}, "gateway": {"192.168.1.1"},
	})
	postForm(t, srv, "/hosts", url.Values{
		"name": {"nas"}, "type": {"physical"}, "ips": {"192.168.1.5"},
	})
	postForm(t, srv, "/hosts", url.Values{
		"name": {"router"}, "type": {"physical"}, "ips": {"10.0.0.9"},
	})

	body := getPageBody(t, srv, "/networks/1")
	for _, want := range []string{"IP allocations", "192.168.1.5", "nas", "192.168.1.2"} {
		if !strings.Contains(body, want) {
			t.Errorf("network detail missing %q\n---\n%s", want, body)
		}
	}
	if strings.Contains(body, "10.0.0.9") {
		t.Errorf("out-of-subnet IP 10.0.0.9 should not appear in this network's IPAM")
	}
}

func TestCreateAndListRelationship(t *testing.T) {
	srv := newTestServer(t)
	// Seed a host (id 1) and a service (id 1).
	postForm(t, srv, "/hosts", url.Values{"name": {"proxmox"}, "type": {"physical"}})
	postForm(t, srv, "/services", url.Values{"name": {"jellyfin"}, "kind": {"container"}})

	rec := postForm(t, srv, "/relationships", url.Values{"from": {"service:1"}, "kind": {"runs on"}, "to": {"host:1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /relationships = %d, want 303", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/relationships", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "runs on") || !strings.Contains(body, "service: jellyfin") || !strings.Contains(body, "host: proxmox") {
		t.Errorf("GET /relationships missing the created relationship or its labels")
	}
}

func TestCreateRelationshipInvalidShowsError(t *testing.T) {
	srv := newTestServer(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"proxmox"}, "type": {"physical"}})
	// self-reference host:1 -> host:1 is invalid
	rec := postForm(t, srv, "/relationships", url.Values{"from": {"host:1"}, "kind": {"depends on"}, "to": {"host:1"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid POST /relationships = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "cannot relate to itself") {
		t.Error("invalid POST /relationships missing validation error")
	}
}

func TestChecksView(t *testing.T) {
	srv := newTestServer(t)
	// A service with no backup link.
	postForm(t, srv, "/services", url.Values{"name": {"lonely-svc"}, "kind": {"container"}})
	// A certificate expiring in 7 days (relative to now, so the test is date-stable).
	soon := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	postForm(t, srv, "/certificates", url.Values{"subject": {"soon.example.com"}, "expires_on": {soon}})

	req := httptest.NewRequest(http.MethodGet, "/checks", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /checks = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "lonely-svc") {
		t.Error("checks page should list the service without a backup")
	}
	if !strings.Contains(body, "soon.example.com") {
		t.Error("checks page should list the soon-expiring certificate")
	}
}

func TestImpactView(t *testing.T) {
	srv := newTestServer(t)
	// host:1, service:1, and service runs on host
	postForm(t, srv, "/hosts", url.Values{"name": {"proxmox"}, "type": {"physical"}})
	postForm(t, srv, "/services", url.Values{"name": {"jellyfin"}, "kind": {"container"}})
	postForm(t, srv, "/relationships", url.Values{"from": {"service:1"}, "kind": {"runs on"}, "to": {"host:1"}})

	req := httptest.NewRequest(http.MethodGet, "/impact?ref=host:1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /impact = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "service: jellyfin") {
		t.Error("impact of host:1 should list the dependent service")
	}
}

func TestHostDetailWithTagsAndNotes(t *testing.T) {
	srv := newTestServer(t)
	// host:1 with a Markdown note
	postForm(t, srv, "/hosts", url.Values{"name": {"proxmox"}, "type": {"physical"}, "notes": {"# Runbook\n\nreboot **carefully**"}})

	// add a tag via the top-level endpoint
	rec := postForm(t, srv, "/tags", url.Values{"entity_type": {"host"}, "entity_id": {"1"}, "tag": {"#Critical"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /tags = %d, want 303", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/hosts/1", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /hosts/1 = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "proxmox") {
		t.Error("detail page missing host name")
	}
	if !strings.Contains(body, "<strong>carefully</strong>") {
		t.Error("notes not rendered as Markdown")
	}
	if !strings.Contains(body, "#critical") {
		t.Error("normalized tag not shown")
	}
}

func TestServiceDetailPage(t *testing.T) {
	srv := newTestServer(t)
	postForm(t, srv, "/services", url.Values{
		"name": {"jellyfin"}, "kind": {"container"}, "url": {"http://jf.local"},
		"notes": {"runs on **proxmox**"},
	})

	req := httptest.NewRequest(http.MethodGet, "/services/1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /services/1 = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "jellyfin") {
		t.Error("detail page missing service name")
	}
	if !strings.Contains(body, "<strong>proxmox</strong>") {
		t.Error("notes not rendered as Markdown")
	}
	if !strings.Contains(body, "/services/1/edit") {
		t.Error("detail page missing edit link")
	}
}

func TestNetworkDetailPage(t *testing.T) {
	srv := newTestServer(t)
	postForm(t, srv, "/networks", url.Values{
		"name": {"lan"}, "cidr": {"10.0.0.0/24"}, "gateway": {"10.0.0.1"},
		"notes": {"main **LAN**"},
	})

	req := httptest.NewRequest(http.MethodGet, "/networks/1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /networks/1 = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "10.0.0.0/24") {
		t.Error("detail page missing CIDR")
	}
	if !strings.Contains(body, "<strong>LAN</strong>") {
		t.Error("notes not rendered as Markdown")
	}
}

func TestExportImport(t *testing.T) {
	srv := newTestServer(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"proxmox"}, "type": {"physical"}, "notes": {"# run"}})

	// Export returns a YAML attachment containing the host.
	req := httptest.NewRequest(http.MethodGet, "/export", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /export = %d, want 200", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "almanaut-export.yaml") {
		t.Errorf("missing attachment filename, got %q", cd)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/x-yaml") {
		t.Errorf("Content-Type = %q, want application/x-yaml", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "proxmox") || !strings.Contains(body, "version: 1") {
		t.Fatalf("export body unexpected:\n%s", body)
	}

	// Import a fresh YAML into a new server → the entity appears.
	yamlDoc := "version: 1\nhosts:\n  - id: 9\n    name: imported-host\n    type: vm\n    ips: []\n    notes: \"\"\nservices: []\nnetworks: []\ndomains: []\ncertificates: []\nbackups: []\nrelationships: []\ntags: []\n"

	srv2 := newTestServer(t)
	rec = uploadImport(t, srv2, yamlDoc, true)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /import = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/hosts", nil)
	rec = httptest.NewRecorder()
	srv2.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "imported-host") {
		t.Error("imported host not listed")
	}

	// Without the confirm checkbox → 400, nothing happens.
	rec = uploadImport(t, srv2, yamlDoc, false)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("import without confirm = %d, want 400", rec.Code)
	}
}

func TestImportReplacesAndRejectsBadYAML(t *testing.T) {
	srv := newTestServer(t)
	// Pre-existing data that must be GONE after a replace-all import.
	postForm(t, srv, "/hosts", url.Values{"name": {"old-host"}, "type": {"physical"}})

	yamlDoc := "version: 1\nhosts:\n  - id: 5\n    name: new-host\n    type: vm\n    ips: []\n    notes: \"\"\nservices: []\nnetworks: []\ndomains: []\ncertificates: []\nbackups: []\nrelationships: []\ntags: []\n"
	if rec := uploadImport(t, srv, yamlDoc, true); rec.Code != http.StatusSeeOther {
		t.Fatalf("import = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/hosts", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "new-host") {
		t.Error("imported host missing after replace-all")
	}
	if strings.Contains(body, "old-host") {
		t.Error("replace-all did not remove pre-existing data")
	}

	// Malformed YAML must render the page with an error (200), not crash (500).
	rec = uploadImport(t, srv, "{ this is not valid yaml", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("malformed YAML = %d, want 200 page render", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid YAML") {
		t.Errorf("expected an 'invalid YAML' error message on the page")
	}
}

// uploadImport posts a multipart form with the YAML file and optional confirm.
func uploadImport(t *testing.T, srv http.Handler, yamlDoc string, confirm bool) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "import.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(yamlDoc)); err != nil {
		t.Fatal(err)
	}
	if confirm {
		if err := mw.WriteField("confirm", "on"); err != nil {
			t.Fatal(err)
		}
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestDashboard(t *testing.T) {
	srv := newTestServer(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"proxmox"}, "type": {"physical"}, "status": {"running"}})
	postForm(t, srv, "/hosts", url.Values{"name": {"oldbox"}, "type": {"vm"}, "status": {"down"}})
	soon := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	postForm(t, srv, "/certificates", url.Values{"subject": {"example.com"}, "expires_on": {soon}})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200 (dashboard, not a redirect)", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Dashboard", "Hosts", "Services", "Certificates", "Attention"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	if !strings.Contains(body, `<span class="card-count">2</span>`) {
		t.Errorf("expected a count card showing 2 hosts; body:\n%s", body)
	}
	// down host appears under attention, linked to its detail page
	if !strings.Contains(body, "/hosts/2") || !strings.Contains(body, "oldbox") {
		t.Error("down host not shown in attention")
	}
	// expiring cert appears under attention, linked to its detail page
	if !strings.Contains(body, "/certificates/1") || !strings.Contains(body, "example.com") {
		t.Error("expiring certificate not shown in attention")
	}
}

func TestDashboardEmpty(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<span class="card-count">0</span>`) {
		t.Error("empty inventory should show zero counts")
	}
	if !strings.Contains(body, "All clear.") {
		t.Error("empty inventory should show an 'All clear.' attention line")
	}
}

func TestDeleteEntityCleansUpRelationshipsAndTags(t *testing.T) {
	srv := newTestServer(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"proxmox"}, "type": {"physical"}})
	postForm(t, srv, "/services", url.Values{"name": {"jellyfin"}, "kind": {"container"}})
	// relationship service:1 -> host:1, and a tag on host:1
	postForm(t, srv, "/relationships", url.Values{"from": {"service:1"}, "to": {"host:1"}, "kind": {"runs on"}})
	postForm(t, srv, "/tags", url.Values{"entity_type": {"host"}, "entity_id": {"1"}, "tag": {"critical"}})

	// delete host:1
	rec := postForm(t, srv, "/hosts/1/delete", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete host = %d, want 303", rec.Code)
	}

	// the relationship touching host:1 must be gone — an orphaned row would render
	// the endpoint as "host:1 (deleted)", so "(deleted)" must NOT appear.
	req := httptest.NewRequest(http.MethodGet, "/relationships", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "(deleted)") {
		t.Error("relationship touching the deleted host was not cleaned up")
	}

	// the host's tag must be gone from the tags overview
	req = httptest.NewRequest(http.MethodGet, "/tags", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "critical") {
		t.Error("tag on the deleted host was not cleaned up")
	}
}

func TestGlobalSearch(t *testing.T) {
	srv := newTestServer(t)
	// host:1 — matches by name, by note content, and by IP; also carries a tag.
	postForm(t, srv, "/hosts", url.Values{
		"name": {"proxmox"}, "type": {"physical"},
		"notes": {"runs the media stack"}, "ips": {"10.0.0.5"},
	})
	postForm(t, srv, "/tags", url.Values{"entity_type": {"host"}, "entity_id": {"1"}, "tag": {"critical"}})
	// service:1 — distinct entity, matched by name.
	postForm(t, srv, "/services", url.Values{"name": {"jellyfin"}, "kind": {"container"}})

	get := func(q string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, "/search?q="+url.QueryEscape(q), nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	// name match → host links to its detail page, under a Hosts heading
	if code, body := get("proxmox"); code != http.StatusOK ||
		!strings.Contains(body, "/hosts/1") || !strings.Contains(body, "Hosts") {
		t.Errorf("search proxmox: code=%d body=%q", code, body)
	}
	// note-content match
	if _, body := get("media"); !strings.Contains(body, "/hosts/1") {
		t.Error("search by note content failed")
	}
	// IP match
	if _, body := get("10.0.0.5"); !strings.Contains(body, "/hosts/1") {
		t.Error("search by IP failed")
	}
	// tag match pulls in the entity
	if _, body := get("critical"); !strings.Contains(body, "/hosts/1") {
		t.Error("search by tag failed")
	}
	// distinct entity type
	if _, body := get("jellyfin"); !strings.Contains(body, "/services/1") {
		t.Error("search service failed")
	}
	// dedupe: "proxmox" matches both the name AND (separately) we tag it "proxmox"
	postForm(t, srv, "/tags", url.Values{"entity_type": {"host"}, "entity_id": {"1"}, "tag": {"proxmox"}})
	if _, body := get("proxmox"); strings.Count(body, `href="/hosts/1"`) != 1 {
		t.Errorf("expected host:1 once, got %d", strings.Count(body, `href="/hosts/1"`))
	}
	// empty query → prompt, no results rows
	if code, body := get(""); code != http.StatusOK || !strings.Contains(body, "Type a query") {
		t.Errorf("empty query: code=%d body=%q", code, body)
	}
	// no match → explicit empty state
	if _, body := get("zzzznope"); !strings.Contains(body, "No results") {
		t.Error("no-match state missing")
	}
}

func TestDiscoveryDockerImport(t *testing.T) {
	scanner := fakeScanner{containers: []discovery.Container{
		{ID: "c1", Name: "jellyfin"},
		{ID: "c2", Name: "sonarr"},
	}}
	srv := newTestServerWithScanner(t, scanner)
	postForm(t, srv, "/hosts", url.Values{"name": {"proxmox"}, "type": {"physical"}}) // host id 1

	// Import only c1, attached to host 1.
	rec := postForm(t, srv, "/discovery/docker/import", url.Values{"id": {"c1"}, "host": {"1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST import = %d, want 303", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "jellyfin") {
		t.Error("selected container jellyfin was not imported")
	}
	if strings.Contains(body, "sonarr") {
		t.Error("unselected container sonarr should not be imported")
	}

	req = httptest.NewRequest(http.MethodGet, "/relationships", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	rbody := rec.Body.String()
	if !strings.Contains(rbody, "runs on") ||
		!strings.Contains(rbody, "service: jellyfin") ||
		!strings.Contains(rbody, "host: proxmox") {
		t.Errorf("expected a 'service jellyfin runs on host proxmox' relationship; body=%q", rbody)
	}
}

func TestDiscoveryDockerImportSkipsAlreadyTracked(t *testing.T) {
	scanner := fakeScanner{containers: []discovery.Container{{ID: "c1", Name: "jellyfin"}}}
	srv := newTestServerWithScanner(t, scanner)
	postForm(t, srv, "/services", url.Values{"name": {"jellyfin"}, "kind": {"container"}})

	// Attempt to import c1 even though a service named jellyfin already exists.
	rec := postForm(t, srv, "/discovery/docker/import", url.Values{"id": {"c1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST import = %d, want 303", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if n := strings.Count(rec.Body.String(), ">jellyfin<"); n != 1 {
		t.Errorf("jellyfin appears %d times, want 1 (no duplicate import)", n)
	}
}

func TestDiscoveryDockerImportSkipsNamelessContainer(t *testing.T) {
	scanner := fakeScanner{containers: []discovery.Container{{ID: "c1", Name: ""}}}
	srv := newTestServerWithScanner(t, scanner)
	rec := postForm(t, srv, "/discovery/docker/import", url.Values{"id": {"c1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST import = %d, want 303", rec.Code)
	}
	// No service should have been created from the nameless container.
	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "No services yet.") {
		t.Error("nameless container should not have been imported as a service")
	}
}

// newTestServerDockerDB is like newTestServerWithScanner but also returns the
// underlying *sql.DB so a test can force a mid-import write failure.
func newTestServerDockerDB(t *testing.T, scanner dockerScanner) (http.Handler, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	srv := New(Config{
		Hosts: store.NewHostRepo(db), Services: store.NewServiceRepo(db), Networks: store.NewNetworkRepo(db),
		Domains: store.NewDomainRepo(db), Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db), Accounts: store.NewAccountRepo(db),
		Relationships: store.NewRelationshipRepo(db), Tags: store.NewTagRepo(db), DB: db,
		Logger: log.New(io.Discard, "", 0),
		Docker: scanner, NetScan: fakeNetworkScanner{}, NetOpts: NetDiscoveryOptions{}, Proxmox: fakeProxmoxScanner{}, PVEOpts: ProxmoxOptions{},
	})
	return srv, db
}

func TestDiscoveryDockerImportRollsBackOnRelFailure(t *testing.T) {
	scanner := fakeScanner{containers: []discovery.Container{{ID: "c1", Name: "jellyfin"}}}
	srv, db := newTestServerDockerDB(t, scanner)
	postForm(t, srv, "/hosts", url.Values{"name": {"proxmox"}, "type": {"physical"}}) // host id 1

	// Force the relationship insert to fail mid-import. relationships has no
	// UNIQUE/FK constraint, so drop the table the second write needs: the
	// service insert succeeds inside the transaction, the relationship insert
	// then errors, and the whole import must roll back.
	if _, err := db.Exec("DROP TABLE relationships"); err != nil {
		t.Fatalf("drop relationships: %v", err)
	}

	rec := postForm(t, srv, "/discovery/docker/import", url.Values{"id": {"c1"}, "host": {"1"}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("import with failing relationship = %d, want 500", rec.Code)
	}

	// The Service must NOT persist — no orphan left behind.
	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if strings.Contains(w.Body.String(), "jellyfin") {
		t.Error("service jellyfin persisted despite the failed relationship — transaction did not roll back")
	}
}

func TestNetworkDiscoveryImport(t *testing.T) {
	srv := newTestServerNet(t, fakeNetworkScanner{}, NetDiscoveryOptions{Enabled: true})
	// Import one new host, skip another by not selecting it.
	rec := postForm(t, srv, "/discovery/network/import", url.Values{
		"type": {"vm"},
		"host": {"192.168.1.50|nas.lan|80, 443"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST import = %d, want 303", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/hosts", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "nas.lan") {
		t.Error("selected host was not imported")
	}
	// Detail page should show the chosen type and provenance.
	req = httptest.NewRequest(http.MethodGet, "/hosts/1", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	detail := rec.Body.String()
	if !strings.Contains(detail, "192.168.1.50") {
		t.Error("imported host missing its IP")
	}
	if !strings.Contains(detail, "Open ports: 80, 443") {
		t.Error("imported host missing provenance notes")
	}
}

func TestNetworkDiscoveryImportSkipsAlreadyTracked(t *testing.T) {
	srv := newTestServerNet(t, fakeNetworkScanner{}, NetDiscoveryOptions{Enabled: true})
	postForm(t, srv, "/hosts", url.Values{"name": {"box"}, "type": {"vm"}, "ips": {"192.168.1.50"}})
	// Attempt to import a host whose IP is already tracked.
	postForm(t, srv, "/discovery/network/import", url.Values{
		"type": {"physical"}, "host": {"192.168.1.50|dup.lan|80"},
	})
	req := httptest.NewRequest(http.MethodGet, "/hosts", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "dup.lan") {
		t.Error("host with already-tracked IP should not be imported")
	}
}

func TestNetworkDiscoveryImportDisabledIs404(t *testing.T) {
	srv := newTestServer(t) // disabled
	rec := postForm(t, srv, "/discovery/network/import", url.Values{"type": {"vm"}, "host": {"10.0.0.1|x|22"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled import = %d, want 404", rec.Code)
	}
}

func TestNewFormPagesRender(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{
		"/hosts/new", "/services/new", "/networks/new",
		"/domains/new", "/certificates/new", "/backups/new",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "<form") {
			t.Errorf("GET %s: expected a form in the rendered page", path)
		}
	}
}

func TestCreateAndListHardware(t *testing.T) {
	srv := newTestServer(t)

	// Create via form POST
	form := url.Values{"name": {"APC UPS"}, "kind": {"ups"}, "manufacturer": {"APC"}, "status": {"active"}}
	req := httptest.NewRequest(http.MethodPost, "/hardware", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /hardware status = %d, want 303", rec.Code)
	}

	// List shows the new hardware
	req = httptest.NewRequest(http.MethodGet, "/hardware", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /hardware status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "APC UPS") {
		t.Errorf("GET /hardware body does not contain created hardware")
	}

	// Delete it and verify the list no longer contains it
	rec = postForm(t, srv, "/hardware/1/delete", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /hardware/1/delete = %d, want 303", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/hardware", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "APC UPS") {
		t.Errorf("GET /hardware still contains deleted hardware")
	}
}

func TestCreateHardwareInvalidShowsError(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"name": {""}, "kind": {"ups"}}
	req := httptest.NewRequest(http.MethodPost, "/hardware", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid POST /hardware status = %d, want 200 (re-render with error)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "name is required") {
		t.Errorf("invalid POST /hardware body missing validation error")
	}
}

func TestCreateAndListSubscription(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{"name": {"Hetzner VPS"}, "kind": {"vps"}, "amount": {"12.99"}, "currency": {"EUR"}, "billing_cycle": {"monthly"}, "auto_renew": {"on"}}
	req := httptest.NewRequest(http.MethodPost, "/subscriptions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /subscriptions status = %d, want 303", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/subscriptions", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /subscriptions status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Hetzner VPS") {
		t.Errorf("GET /subscriptions body does not contain created subscription")
	}
}

func TestCreateSubscriptionInvalidShowsError(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"name": {""}, "amount": {"-5"}}
	req := httptest.NewRequest(http.MethodPost, "/subscriptions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid POST status = %d, want 200 (re-render with error)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "name is required") {
		t.Errorf("invalid POST body missing validation error")
	}
}

func TestCreateAndListAccount(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{
		"name":             {"Proxmox root"},
		"kind":             {"admin"},
		"username":         {"root@pam"},
		"password_manager": {"Bitwarden"},
		"secret_ref":       {"Homelab > Proxmox"},
		"status":           {"active"},
	}
	req := httptest.NewRequest(http.MethodPost, "/accounts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /accounts status = %d, want 303", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/accounts", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /accounts status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Proxmox root") {
		t.Errorf("GET /accounts missing created account; body:\n%s", rec.Body.String())
	}
}

func TestCreateAccountInvalidShowsError(t *testing.T) {
	srv := newTestServer(t)

	rec := postForm(t, srv, "/accounts", url.Values{"name": {"   "}})
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid POST /accounts status = %d, want 200 (form re-render)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "name is required") {
		t.Errorf("expected validation error in body; got:\n%s", rec.Body.String())
	}
}
