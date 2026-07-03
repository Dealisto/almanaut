package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"gopkg.in/yaml.v3"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func TestExportEmptyMarshalsEmptyLists(t *testing.T) {
	db := newTestDB(t)
	snap, err := Export(db)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if snap.Version != 1 {
		t.Errorf("Version = %d, want 1", snap.Version)
	}
	out, err := yaml.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"hosts: []", "services: []", "networks: []", "domains: []", "certificates: []", "backups: []", "relationships: []", "tags: []"} {
		if !strings.Contains(string(out), key) {
			t.Errorf("empty inventory: want %q (not null) in:\n%s", key, out)
		}
	}
}

func TestTagRepoListAll(t *testing.T) {
	db := newTestDB(t)
	tags := NewTagRepo(db)
	if err := tags.Add(domain.Tag{EntityType: "host", EntityID: 1, Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := tags.Add(domain.Tag{EntityType: "service", EntityID: 1, Name: "b"}); err != nil {
		t.Fatal(err)
	}
	all, err := tags.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d tags, want 2", len(all))
	}
}

func TestImportRoundTrip(t *testing.T) {
	db := newTestDB(t)
	// Seed via repos so ids are assigned, then snapshot.
	hostID, err := NewHostRepo(db).Create(domain.Host{Name: "proxmox", Type: "physical", IPs: []string{"10.0.0.5"}, Notes: "# run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewServiceRepo(db).Create(domain.Service{Name: "jellyfin", Kind: "container"}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRelationshipRepo(db).Create(domain.Relationship{FromType: "service", FromID: 1, ToType: "host", ToID: hostID, Kind: "runs on"}); err != nil {
		t.Fatal(err)
	}
	if err := NewTagRepo(db).Add(domain.Tag{EntityType: "host", EntityID: hostID, Name: "critical"}); err != nil {
		t.Fatal(err)
	}
	first, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}

	// Import into a different, fresh DB and re-export.
	db2 := newTestDB(t)
	if err := Import(db2, first); err != nil {
		t.Fatalf("Import: %v", err)
	}
	second, err := Export(db2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("round-trip mismatch:\n first=%+v\nsecond=%+v", first, second)
	}
	// id preserved
	h, err := NewHostRepo(db2).Get(hostID)
	if err != nil || h.Name != "proxmox" {
		t.Errorf("host id %d not preserved: %+v err=%v", hostID, h, err)
	}
}

func TestImportInvalidRollsBack(t *testing.T) {
	db := newTestDB(t)
	if _, err := NewHostRepo(db).Create(domain.Host{Name: "keep", Type: "vm"}); err != nil {
		t.Fatal(err)
	}
	// A snapshot with one invalid relationship (bad type) must abort before deleting.
	bad := Snapshot{
		Version: 1,
		Hosts:   []domain.Host{{ID: 1, Name: "new", Type: "vm"}},
		Relationships: []domain.Relationship{
			{ID: 1, FromType: "bogus", FromID: 1, ToType: "host", ToID: 1, Kind: "runs on"},
		},
	}
	if err := Import(db, bad); err == nil {
		t.Fatal("expected import to fail on invalid relationship")
	}
	// pre-existing data untouched
	hosts, err := NewHostRepo(db).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].Name != "keep" {
		t.Errorf("existing data was modified: %+v", hosts)
	}
}

func TestPortabilitySubscriptionRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if _, err := NewSubscriptionRepo(db).Create(domain.Subscription{
		Name: "Hetzner VPS", Kind: "vps", Amount: "12.99", Currency: "EUR",
		BillingCycle: "monthly", RenewalDate: "2027-01-15", AutoRenew: true,
	}); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	snap, err := Export(db)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(snap.Subscriptions) != 1 || snap.Subscriptions[0].Name != "Hetzner VPS" {
		t.Fatalf("export subscriptions = %+v", snap.Subscriptions)
	}

	if err := Import(db, snap); err != nil {
		t.Fatalf("Import: %v", err)
	}
	list, err := NewSubscriptionRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Amount != "12.99" || !list[0].AutoRenew {
		t.Fatalf("after import: %+v", list)
	}
}

func TestPortabilityAccountRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	acc := domain.Account{
		Name: "Grafana admin", Kind: "admin", Username: "admin",
		PasswordManager: "Bitwarden", SecretRef: "Homelab > Grafana",
		URL: "https://grafana.lan", Status: "active", Notes: "shared",
	}
	id, err := NewAccountRepo(db).Create(acc)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	snap, err := Export(db)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(snap.Accounts) != 1 || snap.Accounts[0].Name != "Grafana admin" {
		t.Fatalf("export accounts = %+v", snap.Accounts)
	}

	if err := Import(db, snap); err != nil {
		t.Fatalf("Import: %v", err)
	}

	got, err := NewAccountRepo(db).Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	acc.ID = id
	if got != acc {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, acc)
	}
}

func TestJournalRoundTripsAndImportLogsOneEvent(t *testing.T) {
	db := newTestDB(t)
	if _, err := NewHostRepo(db).Create(domain.Host{Name: "nas", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewJournalRepo(db).Create(domain.JournalEntry{
		EntityType: "host", EntityID: 1, Kind: domain.JournalInfo, Body: "note", CreatedAt: "2026-07-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	snap, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.JournalEntries) != 1 || snap.JournalEntries[0].Body != "note" {
		t.Fatalf("journal not exported: %+v", snap.JournalEntries)
	}

	dst := newTestDB(t)
	if err := Import(dst, snap); err != nil {
		t.Fatal(err)
	}
	entries, err := NewJournalRepo(dst).ListForEntity("host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Body != "note" {
		t.Fatalf("journal not imported: %+v", entries)
	}
	events, err := NewChangelogRepo(dst).ListRecent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != domain.ActionImport {
		t.Fatalf("import should log exactly one import event, got %+v", events)
	}
}

func TestPortabilityHardwareRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if _, err := NewHardwareRepo(db).Create(domain.Hardware{
		Name: "core-switch", Kind: "switch", WarrantyEnd: "2029-09-09",
	}); err != nil {
		t.Fatalf("seed hardware: %v", err)
	}

	snap, err := Export(db)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(snap.Hardware) != 1 || snap.Hardware[0].Name != "core-switch" {
		t.Fatalf("export hardware = %+v", snap.Hardware)
	}

	if err := Import(db, snap); err != nil {
		t.Fatalf("Import: %v", err)
	}
	list, err := NewHardwareRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Name != "core-switch" || list[0].WarrantyEnd != "2029-09-09" {
		t.Fatalf("after import: %+v", list)
	}
}
