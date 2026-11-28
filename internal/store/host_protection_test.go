package store

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"gopkg.in/yaml.v3"
)

func TestHostProtectionPersistsAcrossLegacyUpdatesAndExplicitClear(t *testing.T) {
	repo := newTestRepo(t)
	id, err := repo.Create(domain.Host{Name: "server", Type: "physical", AgentProtectedFields: []string{"ips", "os"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(domain.Host{ID: id, Name: "renamed", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	h, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h.AgentProtectedFields, []string{"ips", "os"}) {
		t.Fatalf("legacy update lost protection: %+v", h)
	}
	h.AgentProtectedFields = []string{"bogus"}
	if err := repo.Update(h); err == nil {
		t.Fatal("invalid protection accepted")
	}
	h.AgentProtectedFields = []string{}
	if err := repo.Update(h); err != nil {
		t.Fatal(err)
	}
	list, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].AgentProtectedFields == nil || len(list[0].AgentProtectedFields) != 0 {
		t.Fatalf("explicit empty policy not persisted: %+v", list)
	}
}

func TestHostProtectionMigrationPreservesExistingHosts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "0048_host_agent_protection.sql" {
			break
		}
		body, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := applyMigration(db, entry.Name(), string(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO hosts (id, name, type, ips) VALUES (17, 'legacy', 'physical', '["192.0.2.9"]')`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db, path); err != nil {
		t.Fatal(err)
	}
	h, err := NewHostRepo(db).Get(17)
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "legacy" || !reflect.DeepEqual(h.IPs, []string{"192.0.2.9"}) || h.AgentProtectedFields == nil || len(h.AgentProtectedFields) != 0 {
		t.Fatalf("migration changed existing host: %+v", h)
	}
}

func TestHostProtectionYAMLRoundTripAndLegacyImport(t *testing.T) {
	db := newTestDB(t)
	id, err := NewHostRepo(db).Create(domain.Host{Name: "server", Type: "physical", AgentProtectedFields: []string{"ips", "ram"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Version != 4 {
		t.Fatalf("export version = %d", snapshot.Version)
	}
	raw, err := yaml.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Snapshot
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	other := newTestDB(t)
	if err := Import(other, decoded); err != nil {
		t.Fatal(err)
	}
	h, err := NewHostRepo(other).Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h.AgentProtectedFields, []string{"ips", "ram"}) {
		t.Fatalf("protection lost: %+v", h)
	}
	decoded.Hosts[0].AgentProtectedFields = []string{"invalid"}
	if err := Import(other, decoded); err == nil {
		t.Fatal("invalid imported protection accepted")
	}
	h, err = NewHostRepo(other).Get(id)
	if err != nil || !h.AgentProtects("ips") {
		t.Fatalf("failed import changed inventory: %+v, %v", h, err)
	}
	for _, version := range []int{1, 2, 3} {
		decoded.Version = version
		decoded.Hosts[0].AgentProtectedFields = nil
		if err := Import(other, decoded); err != nil {
			t.Fatalf("import version %d: %v", version, err)
		}
		h, err = NewHostRepo(other).Get(id)
		if err != nil || len(h.AgentProtectedFields) != 0 {
			t.Fatalf("legacy import policy = %+v, %v", h, err)
		}
	}
}
