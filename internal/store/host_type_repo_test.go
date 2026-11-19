package store

import (
	"database/sql"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"gopkg.in/yaml.v3"
)

func TestHostTypeCatalogLifecycle(t *testing.T) {
	db := newTestDB(t)
	types, hosts := NewHostTypeRepo(db), NewHostRepo(db)
	defaults, err := types.List()
	if err != nil || !reflect.DeepEqual(defaults, domain.DefaultHostTypes()) {
		t.Fatalf("defaults = %+v, %v", defaults, err)
	}
	custom := domain.HostType{Name: "container", Label: "Container"}
	if err := types.Create(custom); err != nil {
		t.Fatal(err)
	}
	if err := types.Create(custom); err == nil {
		t.Fatal("duplicate type accepted")
	}
	for _, bad := range []domain.HostType{{Name: "bad/name", Label: "Bad"}, {Name: "blank", Label: " "}} {
		if err := types.Create(bad); err == nil {
			t.Fatalf("invalid type accepted: %+v", bad)
		}
	}
	if _, err := hosts.Create(domain.Host{Name: "unknown", Type: "unknown"}); err == nil {
		t.Fatal("unknown type accepted")
	}
	id, err := hosts.Create(domain.Host{Name: "app", Type: "container"})
	if err != nil {
		t.Fatal(err)
	}
	if err := types.UpdateLabel("container", "Application container"); err != nil {
		t.Fatal(err)
	}
	host, err := hosts.Get(id)
	if err != nil || host.Type != "container" || host.TypeLabel != "Application container" {
		t.Fatalf("renamed host = %+v, %v", host, err)
	}
	if err := types.Delete("container"); err == nil {
		t.Fatal("in-use type deleted")
	}
	host.Type = "missing"
	if err := hosts.Update(host); err == nil {
		t.Fatal("unknown type accepted on update")
	}
	host.Type = "vm"
	if err := hosts.Update(host); err != nil {
		t.Fatal(err)
	}
	if err := types.Delete("container"); err != nil {
		t.Fatal(err)
	}
	if _, err := types.Get("container"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted type: %v", err)
	}
	if err := types.UpdateLabel("container", "Gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	for _, name := range domain.HostTypes {
		if err := types.UpdateLabel(name, "Renamed "+name); err != nil {
			t.Fatal(err)
		}
		if err := types.Delete(name); err == nil {
			t.Fatalf("deleted built-in %s", name)
		}
	}
}

func TestHostTypeSnapshotRoundTripAndLegacy(t *testing.T) {
	db := newTestDB(t)
	types := NewHostTypeRepo(db)
	if err := types.Create(domain.HostType{Name: "container", Label: "Container"}); err != nil {
		t.Fatal(err)
	}
	if err := types.UpdateLabel("vm", "Virtual machine"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHostRepo(db).Create(domain.Host{Name: "app", Type: "container"}); err != nil {
		t.Fatal(err)
	}
	snap, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := yaml.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Snapshot
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	target := newTestDB(t)
	if err := Import(target, decoded); err != nil {
		t.Fatal(err)
	}
	restored, err := Export(target)
	if err != nil || !reflect.DeepEqual(snap, restored) {
		t.Fatalf("round trip differs: %v\n%+v\n%+v", err, snap, restored)
	}
	// Older exports still restore and reset the catalog to the original defaults.
	legacy := Snapshot{Version: 1, Hosts: []domain.Host{{ID: 5, Name: "old", Type: "vm"}}}
	if err := Import(target, legacy); err != nil {
		t.Fatal(err)
	}
	got, err := NewHostTypeRepo(target).List()
	if err != nil || !reflect.DeepEqual(got, domain.DefaultHostTypes()) {
		t.Fatalf("legacy catalog: %+v, %v", got, err)
	}
}

func TestHostTypeInvalidSnapshotLeavesInventoryUntouched(t *testing.T) {
	db := newTestDB(t)
	if _, err := NewHostRepo(db).Create(domain.Host{Name: "keep", Type: "vm"}); err != nil {
		t.Fatal(err)
	}
	original, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		types    []domain.HostType
		hostType string
	}{
		{"missing catalog", nil, "vm"},
		{"missing built-in", domain.DefaultHostTypes()[1:], "vm"},
		{"unknown reference", domain.DefaultHostTypes(), "unknown"},
		{"duplicate key", append(domain.DefaultHostTypes(), domain.HostType{Name: "vm", Label: "Duplicate"}), "vm"},
		{"invalid label", append(domain.DefaultHostTypes(), domain.HostType{Name: "new", Label: " "}), "vm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Import(db, Snapshot{Version: 2, HostTypes: tc.types, Hosts: []domain.Host{{ID: 2, Name: "new", Type: tc.hostType}}})
			if err == nil {
				t.Fatal("invalid snapshot accepted")
			}
			after, err := Export(db)
			if err != nil || !reflect.DeepEqual(original, after) {
				t.Fatalf("inventory changed: %v", err)
			}
		})
	}
}

func TestHostTypeDeleteRacingHostCreate(t *testing.T) {
	db := newTestDB(t)
	types, hosts := NewHostTypeRepo(db), NewHostRepo(db)
	if err := types.Create(domain.HostType{Name: "container", Label: "Container"}); err != nil {
		t.Fatal(err)
	}
	var createErr, deleteErr error
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, createErr = hosts.Create(domain.Host{Name: "app", Type: "container"})
	}()
	go func() { defer wg.Done(); <-start; deleteErr = types.Delete("container") }()
	close(start)
	wg.Wait()
	if (createErr == nil) == (deleteErr == nil) {
		t.Fatalf("expected one winner: create=%v delete=%v", createErr, deleteErr)
	}
	for _, err := range []error{createErr, deleteErr} {
		if err != nil {
			var constraint *ConstraintError
			if !errors.As(err, &constraint) {
				t.Fatalf("expected validation failure: %v", err)
			}
		}
	}
	var orphans int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hosts h LEFT JOIN host_types t ON t.name=h.type WHERE t.name IS NULL`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatalf("orphans=%d, %v", orphans, err)
	}
}

func TestHostTypeMigrationPreservesExistingHosts(t *testing.T) {
	db := newTestDB(t)
	// Reapply only this migration to the schema immediately before the catalog.
	if _, err := db.Exec(`DROP TABLE host_types`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX idx_hosts_type`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO hosts(name,type) VALUES ('legacy','container'), ('node','physical')`); err != nil {
		t.Fatal(err)
	}
	migration, err := migrationsFS.ReadFile("migrations/0039_host_types.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := WithTx(db, func(tx *sql.Tx) error { _, err := tx.Exec(string(migration)); return err }); err != nil {
		t.Fatal(err)
	}
	got, err := NewHostTypeRepo(db).Get("container")
	if err != nil || got.Label != "container" {
		t.Fatalf("legacy type: %+v, %v", got, err)
	}
	hosts, err := NewHostRepo(db).List()
	if err != nil || len(hosts) != 2 {
		t.Fatalf("migration lost hosts: %+v, %v", hosts, err)
	}
}
