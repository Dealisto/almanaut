package web

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestMetricsEndpoint(t *testing.T) {
	h, db := newTestServerDockerDB(t, fakeScanner{})
	hosts := store.NewHostRepo(db)
	if _, err := hosts.Create(domain.Host{Name: "up-1", Type: "vm", Status: "running"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := hosts.Create(domain.Host{Name: "down-1", Type: "vm", Status: "down"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	soon := time.Now().Add(5 * 24 * time.Hour).Format("2006-01-02")
	if _, err := store.NewCertificateRepo(db).Create(domain.Certificate{Subject: "x", ExpiresOn: soon}); err != nil {
		t.Fatalf("create cert: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type = %q", ct)
	}
	body := rec.Body.String()
	wants := []string{
		"# TYPE almanaut_entities_total gauge",
		`almanaut_entities_total{type="host"} 2`,
		`almanaut_entities_total{type="account"} 0`,
		"# TYPE almanaut_hosts_down_total gauge",
		"almanaut_hosts_down_total 1",
		"almanaut_certificates_expiring_total 1",
		"almanaut_relationships_total 0",
		"almanaut_services_without_backup_total 0",
	}
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("metrics body missing %q\n---\n%s", w, body)
		}
	}
}

// TestMetricsBehindAuth verifies /metrics sits inside the same
// basic-auth-protected route group as the rest of the UI when auth is
// configured, unlike /healthz and /version which are intentionally
// unauthenticated.
func TestMetricsBehindAuth(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	h := New(Config{
		Hosts: store.NewHostRepo(db), Services: store.NewServiceRepo(db), Networks: store.NewNetworkRepo(db),
		Domains: store.NewDomainRepo(db), Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db), Accounts: store.NewAccountRepo(db),
		Relationships: store.NewRelationshipRepo(db), Tags: store.NewTagRepo(db), DB: db,
		Logger: log.New(io.Discard, "", 0),
		Docker: fakeScanner{}, NetScan: fakeNetworkScanner{}, NetOpts: NetDiscoveryOptions{}, Proxmox: fakeProxmoxScanner{}, PVEOpts: ProxmoxOptions{},
		AuthUser: "u", AuthPass: "p",
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
}

// TestMetricsErrorReturns500 verifies a repo failure surfaces as a 500 with
// no partial metrics body, rather than a truncated 200.
func TestMetricsErrorReturns500(t *testing.T) {
	h, db := newTestServerDockerDB(t, fakeScanner{})

	if _, err := db.Exec("DROP TABLE hosts"); err != nil {
		t.Fatalf("drop hosts: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "almanaut_entities_total") {
		t.Errorf("body should not contain a partial metrics line: %s", rec.Body.String())
	}
}
