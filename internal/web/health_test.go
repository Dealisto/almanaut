package web

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
)

// newTestServerAuthEnabled builds a server with session auth enabled against a
// freshly migrated db, so tests can assert auth gating.
func newTestServerAuthEnabled(t *testing.T) http.Handler {
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
	return newAuthedTestHandler(t, db)
}

func TestHealthzOK(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal /healthz body: %v; body=%s", err, rec.Body.String())
	}
	if got.Status != "ok" {
		t.Errorf("status = %q, want \"ok\"", got.Status)
	}
	if got.Version == "" {
		t.Errorf("version is empty, want the running version")
	}
}

func TestVersionEndpoint(t *testing.T) {
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
		Sites: store.NewSiteRepo(db), Locations: store.NewLocationRepo(db), Racks: store.NewRackRepo(db),
		Contacts:      store.NewContactRepo(db),
		Relationships: store.NewRelationshipRepo(db), Tags: store.NewTagRepo(db), VLANs: store.NewVLANRepo(db), Reservations: store.NewReservationRepo(db), DB: db,
		Logger: log.New(io.Discard, "", 0),
		Docker: fakeScanner{}, NetScan: fakeNetworkScanner{}, Proxmox: fakeProxmoxScanner{},
		Version: "v9.9.9",
	})
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
// when session auth is enabled, otherwise a container HEALTHCHECK would fail.
func TestHealthAndVersionBypassAuth(t *testing.T) {
	srv := newTestServerAuthEnabled(t)

	// A protected page must require auth (sanity: auth is actually on).
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET / without a session = %d, want 303 (auth enabled)", rec.Code)
	}

	for _, path := range []string{"/healthz", "/version"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s without a session = %d, want 200 (must bypass auth)", path, rec.Code)
		}
	}
}
