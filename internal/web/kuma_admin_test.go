package web

import (
	"database/sql"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/kuma"
	"github.com/Dealisto/almanaut/internal/store"
)

type fakeSyncer struct {
	triggered int
	last      kuma.LastSync
}

func (f *fakeSyncer) TriggerSync()            { f.triggered++ }
func (f *fakeSyncer) LastSync() kuma.LastSync { return f.last }

// newAuthedTestHandlerWithKuma mirrors newAuthedTestHandler but also wires the
// Kuma admin page, for tests that need it enabled.
func newAuthedTestHandlerWithKuma(t *testing.T, db *sql.DB, opts KumaOptions) http.Handler {
	t.Helper()
	return New(Config{
		Hosts: store.NewHostRepo(db), Services: store.NewServiceRepo(db), Networks: store.NewNetworkRepo(db),
		Domains: store.NewDomainRepo(db), Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db), Accounts: store.NewAccountRepo(db),
		Sites: store.NewSiteRepo(db), Locations: store.NewLocationRepo(db), Racks: store.NewRackRepo(db),
		Contacts:      store.NewContactRepo(db),
		Relationships: store.NewRelationshipRepo(db), Tags: store.NewTagRepo(db), VLANs: store.NewVLANRepo(db), Reservations: store.NewReservationRepo(db), DB: db,
		Logger: log.New(io.Discard, "", 0),
		Docker: fakeScanner{}, NetScan: fakeNetworkScanner{}, NetOpts: NetDiscoveryOptions{}, Proxmox: fakeProxmoxScanner{}, PVEOpts: ProxmoxOptions{},
		AuthEnabled: true,
		Kuma:        opts,
	})
}

func TestKumaRoutesAbsentWhenDisabled(t *testing.T) {
	db := rbacDB(t)
	h := newAuthedTestHandler(t, db) // Kuma left as zero value: disabled
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)
	if rec := getWith(t, h, admin, "/kuma"); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /kuma with kuma disabled = %d, want 404", rec.Code)
	}
}

func TestKumaPageRequiresAdmin(t *testing.T) {
	db := rbacDB(t)
	fs := &fakeSyncer{}
	h := newAuthedTestHandlerWithKuma(t, db, KumaOptions{Enabled: true, URL: "http://kuma.lan:3001", Syncer: fs})
	viewer := seedUserAndLogin(t, h, db, "viewer", domain.RoleViewer)
	if rec := getWith(t, h, viewer, "/kuma"); rec.Code != http.StatusForbidden {
		t.Fatalf("GET /kuma as viewer = %d, want 403", rec.Code)
	}
}

func TestKumaPageRendersWhenEnabled(t *testing.T) {
	db := rbacDB(t)
	fs := &fakeSyncer{}
	h := newAuthedTestHandlerWithKuma(t, db, KumaOptions{Enabled: true, URL: "http://kuma.lan:3001", Syncer: fs})
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)
	rec := getWith(t, h, admin, "/kuma")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /kuma = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "http://kuma.lan:3001") {
		t.Fatal("page does not show the configured Kuma URL")
	}
	if !strings.Contains(body, "never") { // no pass has run yet
		t.Fatal("page does not show the never-synced state")
	}
}

func TestKumaSyncNowTriggers(t *testing.T) {
	db := rbacDB(t)
	fs := &fakeSyncer{}
	h := newAuthedTestHandlerWithKuma(t, db, KumaOptions{Enabled: true, URL: "http://kuma.lan:3001", Syncer: fs})
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)
	rec := csrfPostRec(t, h, admin, "/kuma/sync", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /kuma/sync = %d, want 303", rec.Code)
	}
	if fs.triggered != 1 {
		t.Fatalf("TriggerSync calls = %d, want 1", fs.triggered)
	}
}
