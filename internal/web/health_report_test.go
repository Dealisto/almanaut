package web

import (
	"database/sql"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/store"
)

// newStaleTestServer builds a server with the stale-entity rule enabled at the
// given window, returning the db so a test can age changelog rows.
func newStaleTestServer(t *testing.T, days int) (http.Handler, *sql.DB) {
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
		StaleAfterDays: days,
	})
	return srv, db
}

// TestHealthReportEmpty asserts an empty inventory renders every rule and the
// "All clear" summary badge.
func TestHealthReportEmpty(t *testing.T) {
	srv, _ := newTestServerDB(t)
	body := getBody(t, srv, "/health-report")
	for _, want := range []string{
		"Health report", "All clear",
		"Hosts without a backup", "Services not linked to a host",
		"Expired certificates", "Certificates linked to nothing",
		"Hardware without a warranty date", "Subscriptions without a renewal date",
		"Orphaned entities",
		"Duplicate IP assignments", "Host IPs outside every network", "Overlapping networks",
		"Stale entities",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("health report missing %q:\n%s", want, body)
		}
	}
}

// TestHealthReportFindings drives real creates and asserts the offending
// entities surface under their rule and are counted on the dashboard.
func TestHealthReportFindings(t *testing.T) {
	srv, _ := newTestServerDB(t)

	// A host with no backup relationship, an already-expired certificate, and a
	// piece of hardware with no warranty date — one offender per several rules.
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"lonely-nas"}, "type": {"physical"}, "status": {"running"}}); rec.Code != 303 {
		t.Fatalf("POST /hosts = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/certificates", url.Values{"subject": {"old-cert"}, "expires_on": {"2000-01-01"}}); rec.Code != 303 {
		t.Fatalf("POST /certificates = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/hardware", url.Values{"name": {"mystery-box"}}); rec.Code != 303 {
		t.Fatalf("POST /hardware = %d", rec.Code)
	}

	body := getBody(t, srv, "/health-report")
	for _, want := range []string{"lonely-nas", "old-cert", "mystery-box"} {
		if !strings.Contains(body, want) {
			t.Errorf("health report missing offender %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "All clear") {
		t.Error("expected findings, but report shows All clear")
	}

	// The dashboard summarizes the same findings and links to the report.
	dash := getBody(t, srv, "/")
	if !strings.Contains(dash, "/health-report") || !strings.Contains(dash, "inventory") {
		t.Errorf("dashboard missing inventory health summary:\n%s", dash)
	}
}

// TestHealthReportIPAMConflicts drives real creates so all three IPAM conflict
// types surface on the health page.
func TestHealthReportIPAMConflicts(t *testing.T) {
	srv, _ := newTestServerDB(t)

	// Two networks with the identical CIDR block -> overlap conflict.
	if rec := postForm(t, srv, "/networks", url.Values{"name": {"lan"}, "cidr": {"192.168.1.0/24"}}); rec.Code != 303 {
		t.Fatalf("POST /networks lan = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/networks", url.Values{"name": {"lan-dup"}, "cidr": {"192.168.1.0/24"}}); rec.Code != 303 {
		t.Fatalf("POST /networks lan-dup = %d", rec.Code)
	}
	// Two hosts sharing 192.168.1.5 -> duplicate IP; one host on 10.9.9.9 -> outside.
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"h1"}, "type": {"physical"}, "ips": {"192.168.1.5"}}); rec.Code != 303 {
		t.Fatalf("POST /hosts h1 = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"h2"}, "type": {"physical"}, "ips": {"192.168.1.5, 10.9.9.9"}}); rec.Code != 303 {
		t.Fatalf("POST /hosts h2 = %d", rec.Code)
	}

	body := getBody(t, srv, "/health-report")
	for _, want := range []string{
		"192.168.1.5 — h1", "192.168.1.5 — h2", // duplicate IP, one link per host
		"h2 (10.9.9.9)",           // outside every network
		"lan (192.168.1.0/24) ↔ ", // overlapping networks
	} {
		if !strings.Contains(body, want) {
			t.Errorf("health report missing %q:\n%s", want, body)
		}
	}
}

// TestNetworkDetailShowsOverlap asserts the affected network's detail page warns
// about the CIDR it shares.
func TestNetworkDetailShowsOverlap(t *testing.T) {
	srv, _ := newTestServerDB(t)
	if rec := postForm(t, srv, "/networks", url.Values{"name": {"lan"}, "cidr": {"10.0.0.0/24"}}); rec.Code != 303 {
		t.Fatalf("POST /networks lan = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/networks", url.Values{"name": {"lan-dup"}, "cidr": {"10.0.0.0/24"}}); rec.Code != 303 {
		t.Fatalf("POST /networks lan-dup = %d", rec.Code)
	}
	body := getBody(t, srv, "/networks/1")
	if !strings.Contains(body, "shares its CIDR block") || !strings.Contains(body, "lan-dup") {
		t.Errorf("network detail missing overlap warning:\n%s", body)
	}
}

// TestHealthReportStaleAndAcknowledge drives the stale rule end to end: an aged
// entity shows up with an acknowledge button, and acknowledging it (recorded in
// history) resets its clock so the button disappears.
func TestHealthReportStaleAndAcknowledge(t *testing.T) {
	srv, db := newStaleTestServer(t, 90)

	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"old-host"}, "type": {"physical"}}); rec.Code != 303 {
		t.Fatalf("POST /hosts = %d", rec.Code)
	}
	// Age the host's only changelog event well past the window.
	if _, err := db.Exec(
		`UPDATE changelog SET created_at = ? WHERE entity_type = 'host' AND entity_id = 1`,
		"2000-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("age changelog: %v", err)
	}

	body := getBody(t, srv, "/health-report")
	if !strings.Contains(body, "Stale entities") || !strings.Contains(body, "old-host") {
		t.Fatalf("stale rule missing the aged host:\n%s", body)
	}
	if !strings.Contains(body, `value="host:1"`) || !strings.Contains(body, "Acknowledge") {
		t.Fatalf("expected an acknowledge button for host:1:\n%s", body)
	}

	if rec := postForm(t, srv, "/health-report/acknowledge", url.Values{"ref": {"host:1"}}); rec.Code != 303 {
		t.Fatalf("POST acknowledge = %d", rec.Code)
	}

	// The acknowledgement is recorded in history...
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM changelog WHERE entity_type='host' AND entity_id=1 AND action='acknowledge'`,
	).Scan(&n); err != nil {
		t.Fatalf("count ack: %v", err)
	}
	if n != 1 {
		t.Fatalf("acknowledge events = %d, want 1", n)
	}
	// ...and it resets the clock, so the acknowledge button is gone.
	if body := getBody(t, srv, "/health-report"); strings.Contains(body, `value="host:1"`) {
		t.Errorf("host:1 still stale after acknowledge:\n%s", body)
	}
}

// TestAcknowledgeNonexistentEntity verifies acknowledging a ref for an entity
// that does not exist is rejected and writes no changelog row.
func TestAcknowledgeNonexistentEntity(t *testing.T) {
	srv, db := newStaleTestServer(t, 90)
	rec := postForm(t, srv, "/health-report/acknowledge", url.Values{"ref": {"host:999999"}})
	if rec.Code != 404 {
		t.Fatalf("acknowledge nonexistent = %d, want 404", rec.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM changelog WHERE action='acknowledge'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("phantom acknowledge rows = %d, want 0", n)
	}
}

// TestHealthReportStaleDisabled verifies the rule reports nothing when the
// window is 0, even for a very old entity.
func TestHealthReportStaleDisabled(t *testing.T) {
	srv, db := newStaleTestServer(t, 0)
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"ancient"}, "type": {"physical"}}); rec.Code != 303 {
		t.Fatalf("POST /hosts = %d", rec.Code)
	}
	if _, err := db.Exec(
		`UPDATE changelog SET created_at = ? WHERE entity_type='host' AND entity_id=1`,
		"2000-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("age changelog: %v", err)
	}
	body := getBody(t, srv, "/health-report")
	if strings.Contains(body, `value="host:1"`) {
		t.Errorf("stale rule should be disabled at 0 days:\n%s", body)
	}
}

// TestDiscoveryImportRecordsSighting verifies a discovery import writes an
// "import" changelog event, so the import counts as a sighting for stale
// detection and appears in history.
func TestDiscoveryImportRecordsSighting(t *testing.T) {
	scanner := fakeScanner{containers: []discovery.Container{{ID: "c1", Name: "jellyfin"}}}
	srv, db := newTestServerDockerDB(t, scanner)
	if rec := postForm(t, srv, "/discovery/docker/import", url.Values{"id": {"c1"}}); rec.Code != 303 {
		t.Fatalf("docker import = %d", rec.Code)
	}
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM changelog WHERE entity_type='service' AND action='import'`,
	).Scan(&n); err != nil {
		t.Fatalf("count import events: %v", err)
	}
	if n != 1 {
		t.Errorf("import changelog events = %d, want 1", n)
	}
}

// TestHealthReportCertRuleNotDoubleCounted verifies an unlinked certificate
// shows under its dedicated rule but not also under "Orphaned entities".
func TestHealthReportCertRuleNotDoubleCounted(t *testing.T) {
	srv, _ := newTestServerDB(t)
	// Future expiry so it is unlinked but not also "expired"; that way it must
	// surface under exactly one rule.
	if rec := postForm(t, srv, "/certificates", url.Values{"subject": {"floating-cert"}, "expires_on": {"2100-01-01"}}); rec.Code != 303 {
		t.Fatalf("POST /certificates = %d", rec.Code)
	}
	body := getBody(t, srv, "/health-report")
	// It appears once (the cert rule), so exactly one occurrence of the subject.
	if n := strings.Count(body, "floating-cert"); n != 1 {
		t.Errorf("floating-cert appears %d times, want 1 (cert rule only):\n%s", n, body)
	}
}
