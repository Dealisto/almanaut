package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
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
	return New(store.NewHostRepo(db), store.NewServiceRepo(db), store.NewNetworkRepo(db), store.NewDomainRepo(db), store.NewCertificateRepo(db), store.NewBackupRepo(db), store.NewRelationshipRepo(db))
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

func postForm(t *testing.T, srv http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
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
