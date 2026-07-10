package web

import (
	"database/sql"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestParseCIDRList(t *testing.T) {
	nets, skipped := parseCIDRList("10.0.0.0/8, 192.168.1.5 , , not-an-ip")
	if len(skipped) != 1 || skipped[0] != " not-an-ip" {
		t.Errorf("skipped = %+v, want [ not-an-ip]", skipped)
	}
	if !ipInList(net.ParseIP("10.9.9.9"), nets) {
		t.Error("10.9.9.9 should be inside 10.0.0.0/8")
	}
	if !ipInList(net.ParseIP("192.168.1.5"), nets) {
		t.Error("192.168.1.5 (bare IP → /32) should match")
	}
	if ipInList(net.ParseIP("192.168.1.6"), nets) {
		t.Error("192.168.1.6 should not match a /32 for .5")
	}
}

func newProxyTestHandler(t *testing.T, autoProvision bool) (http.Handler, *sql.DB) {
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
		Sites: store.NewSiteRepo(db), Locations: store.NewLocationRepo(db), Racks: store.NewRackRepo(db),
		Contacts:      store.NewContactRepo(db),
		Relationships: store.NewRelationshipRepo(db), Tags: store.NewTagRepo(db), VLANs: store.NewVLANRepo(db), Reservations: store.NewReservationRepo(db), DB: db,
		Logger: log.New(io.Discard, "", 0),
		Docker: fakeScanner{}, NetScan: fakeNetworkScanner{}, NetOpts: NetDiscoveryOptions{}, Proxmox: fakeProxmoxScanner{}, PVEOpts: ProxmoxOptions{},
		AuthEnabled:            true,
		ProxyAuthHeader:        "Remote-User",
		ProxyAuthAllowlist:     "10.0.0.0/8",
		ProxyAuthAutoProvision: autoProvision,
		ProxyAuthDefaultRole:   "viewer",
	})
	return srv, db
}

// getWith issues a GET with a given RemoteAddr and optional Remote-User header.
func getProxy(t *testing.T, h http.Handler, path, remoteAddr, remoteUser string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr
	if remoteUser != "" {
		req.Header.Set("Remote-User", remoteUser)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestProxyAuthTrustedProxyLogsIn(t *testing.T) {
	h, db := newProxyTestHandler(t, false)
	if _, err := store.NewUserRepo(db).Create(domain.User{
		Username: "alice", Role: domain.RoleEditor, PasswordHash: "x", CreatedAt: "t", UpdatedAt: "t",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// Through the trusted proxy → authenticated (200, not a login redirect).
	rec := getProxy(t, h, "/hosts", "10.0.0.5:1234", "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("trusted proxy GET /hosts = %d, want 200", rec.Code)
	}
}

func TestProxyAuthHeaderIgnoredFromUntrustedSource(t *testing.T) {
	h, db := newProxyTestHandler(t, false)
	store.NewUserRepo(db).Create(domain.User{Username: "alice", Role: domain.RoleEditor, PasswordHash: "x", CreatedAt: "t", UpdatedAt: "t"})
	// Same header, but from a non-allowlisted source → ignored → login redirect.
	rec := getProxy(t, h, "/hosts", "192.168.1.1:1234", "alice")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("untrusted source GET = %d, want 303 redirect to login", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc == "" || loc[:6] != "/login" {
		t.Errorf("redirect = %q, want /login…", loc)
	}
}

func TestProxyAuthUnknownUserNoProvision(t *testing.T) {
	h, _ := newProxyTestHandler(t, false)
	// Trusted proxy but unknown identity + provisioning off → fall back to login.
	rec := getProxy(t, h, "/hosts", "10.0.0.5:1234", "ghost")
	if rec.Code != http.StatusSeeOther {
		t.Errorf("unknown user, no provision = %d, want 303", rec.Code)
	}
}

func TestProxyAuthAutoProvision(t *testing.T) {
	h, db := newProxyTestHandler(t, true)
	rec := getProxy(t, h, "/hosts", "10.0.0.5:1234", "newbie")
	if rec.Code != http.StatusOK {
		t.Fatalf("auto-provision GET = %d, want 200", rec.Code)
	}
	var role string
	if err := db.QueryRow(`SELECT role FROM users WHERE username='newbie'`).Scan(&role); err != nil {
		t.Fatalf("provisioned user not found: %v", err)
	}
	if role != "viewer" {
		t.Errorf("provisioned role = %q, want viewer", role)
	}
}
