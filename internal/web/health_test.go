package web

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
)

// newTestServerAuth builds a server with HTTP basic auth enabled and a fixed
// version string, so tests can assert auth gating and the /version payload.
func newTestServerAuth(t *testing.T, user, pass, version string) http.Handler {
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
		Docker: fakeScanner{}, NetScan: fakeNetworkScanner{}, Proxmox: fakeProxmoxScanner{},
		AuthUser: user, AuthPass: pass, Version: version,
	})
}

func TestHealthzOK(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "ok" {
		t.Errorf("GET /healthz body = %q, want \"ok\"", body)
	}
}

func TestVersionEndpoint(t *testing.T) {
	srv := newTestServerAuth(t, "", "", "v9.9.9")
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /version = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "v9.9.9") {
		t.Errorf("GET /version body = %q, want it to contain the version", body)
	}
}

// The liveness and version probes must stay reachable without credentials even
// when basic auth is enabled, otherwise a container HEALTHCHECK would fail.
func TestHealthAndVersionBypassAuth(t *testing.T) {
	srv := newTestServerAuth(t, "admin", "secret", "dev")

	// A protected page must require auth (sanity: auth is actually on).
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET / without creds = %d, want 401 (auth enabled)", rec.Code)
	}

	for _, path := range []string{"/healthz", "/version"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s without creds = %d, want 200 (must bypass auth)", path, rec.Code)
		}
	}
}
