package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"gopkg.in/yaml.v3"
)

func TestDiscoveryBindingsPersistAcrossRenamesAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db, path); err != nil {
		t.Fatal(err)
	}
	hosts := NewHostRepo(db)
	id, err := hosts.Create(domain.Host{Name: "original", Type: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	binding := domain.DiscoveryBinding{Source: "xen-orchestra:lab", ExternalID: "vm-uuid", EntityType: "host", EntityID: id}
	if err := NewDiscoveryBindingRepo(db).Create(binding); err != nil {
		t.Fatal(err)
	}
	host, err := hosts.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	host.Name = "manually renamed"
	host.Notes = "Manual notes replace discovery provenance."
	if err := hosts.Update(host); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	got, err := NewDiscoveryBindingRepo(db).List()
	if err != nil || !reflect.DeepEqual(got, []domain.DiscoveryBinding{binding}) {
		t.Fatalf("binding after restart = %+v, %v", got, err)
	}
}

func TestDiscoveryBindingSourceIsolationAndDeletion(t *testing.T) {
	db := newTestDB(t)
	hosts, services := NewHostRepo(db), NewServiceRepo(db)
	hostID, err := hosts.Create(domain.Host{Name: "host", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	serviceID, err := services.Create(domain.Service{Name: "service", Kind: "container"})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewDiscoveryBindingRepo(db)
	bindings := []domain.DiscoveryBinding{
		{Source: "proxmox:lab-a", ExternalID: "qemu/100", EntityType: "host", EntityID: hostID},
		{Source: "proxmox:lab-b", ExternalID: "qemu/100", EntityType: "host", EntityID: hostID},
		{Source: "docker:default", ExternalID: "container-id", EntityType: "service", EntityID: serviceID},
	}
	for _, b := range bindings {
		if err := repo.Create(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Create(bindings[0]); err == nil {
		t.Fatal("duplicate source/resource identity accepted")
	}
	if err := hosts.Delete(hostID); err != nil {
		t.Fatal(err)
	}
	got, err := repo.List()
	if err != nil || !reflect.DeepEqual(got, bindings[2:]) {
		t.Fatalf("host deletion removed wrong bindings: %+v, %v", got, err)
	}
	if err := services.Delete(serviceID); err != nil {
		t.Fatal(err)
	}
	got, err = repo.List()
	if err != nil || len(got) != 0 {
		t.Fatalf("service deletion left bindings: %+v, %v", got, err)
	}
	newID, err := hosts.Create(domain.Host{Name: "replacement", Type: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	bindings[0].EntityID = newID
	if err := repo.Create(bindings[0]); err != nil {
		t.Fatalf("deleted identity was not released: %v", err)
	}
}

func TestDiscoveryBindingValidationAndAtomicity(t *testing.T) {
	db := newTestDB(t)
	repo := NewDiscoveryBindingRepo(db)
	for _, b := range []domain.DiscoveryBinding{
		{Source: "https://example.test/token", ExternalID: "vm", EntityType: "host", EntityID: 1},
		{Source: "xen-orchestra:lab", ExternalID: "\n", EntityType: "host", EntityID: 1},
		{Source: "xen-orchestra:lab", ExternalID: "vm", EntityType: "network", EntityID: 1},
		{Source: "xen-orchestra:lab", ExternalID: "vm", EntityType: "host", EntityID: 0},
		{Source: "xen-orchestra:lab", ExternalID: "vm", EntityType: "host", EntityID: 999},
	} {
		if err := repo.Create(b); err == nil {
			t.Fatalf("invalid binding accepted: %+v", b)
		}
	}
	if _, err := db.Exec(`INSERT INTO discovery_bindings VALUES ('proxmox:lab', 'node/a', 'host', 999)`); err == nil {
		t.Fatal("database accepted dangling target")
	}
	abort := errors.New("abort import")
	err := WithTx(db, func(tx *sql.Tx) error {
		id, err := NewHostRepo(db).WithTx(tx).Create(domain.Host{Name: "temporary", Type: "vm"})
		if err != nil {
			return err
		}
		if err := repo.WithTx(tx).Create(domain.DiscoveryBinding{Source: "proxmox:lab", ExternalID: "qemu/100", EntityType: "host", EntityID: id}); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM hosts`); n != 0 {
		t.Fatalf("rolled-back hosts = %d", n)
	}
	got, err := repo.List()
	if err != nil || len(got) != 0 {
		t.Fatalf("rolled-back bindings = %+v, %v", got, err)
	}
}

func TestDiscoveryBindingsSnapshotRoundTrip(t *testing.T) {
	db := newTestDB(t)
	hostID, err := NewHostRepo(db).Create(domain.Host{Name: "manual name", Type: "vm", Notes: "Keep this note."})
	if err != nil {
		t.Fatal(err)
	}
	serviceID, err := NewServiceRepo(db).Create(domain.Service{Name: "manual service", Kind: "container"})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []domain.DiscoveryBinding{
		{Source: "xen-orchestra:lab", ExternalID: "uuid", EntityType: "host", EntityID: hostID},
		{Source: "docker:lab", ExternalID: "container-id", EntityType: "service", EntityID: serviceID},
	} {
		if err := NewDiscoveryBindingRepo(db).Create(b); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Version != 3 || len(snapshot.DiscoveryBindings) != 2 {
		t.Fatalf("snapshot identity format = %+v", snapshot)
	}
	data, err := yaml.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Snapshot
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	// Restoring over existing inventory also exercises deletion-trigger ordering.
	if err := Import(db, decoded); err != nil {
		t.Fatal(err)
	}
	got, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.DiscoveryBindings, snapshot.DiscoveryBindings) {
		t.Fatalf("restored bindings = %+v", got.DiscoveryBindings)
	}
	if got.Hosts[0].Name != "manual name" || got.Hosts[0].Notes != "Keep this note." {
		t.Fatal("restore changed manual inventory")
	}
}

func TestDiscoveryBindingsInvalidSnapshotPreservesInventory(t *testing.T) {
	for _, kind := range []string{"missing target", "duplicate identity", "invalid source"} {
		t.Run(kind, func(t *testing.T) {
			db := newTestDB(t)
			id, err := NewHostRepo(db).Create(domain.Host{Name: "keep", Type: "vm"})
			if err != nil {
				t.Fatal(err)
			}
			binding := domain.DiscoveryBinding{Source: "xen-orchestra:lab", ExternalID: "keep-id", EntityType: "host", EntityID: id}
			if err := NewDiscoveryBindingRepo(db).Create(binding); err != nil {
				t.Fatal(err)
			}
			snapshot, err := Export(db)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing target":
				snapshot.DiscoveryBindings[0].EntityID = 999
			case "duplicate identity":
				snapshot.DiscoveryBindings = append(snapshot.DiscoveryBindings, binding)
			case "invalid source":
				snapshot.DiscoveryBindings[0].Source = "https://secret.example"
			}
			if err := Import(db, snapshot); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
			host, err := NewHostRepo(db).Get(id)
			if err != nil || host.Name != "keep" {
				t.Fatalf("inventory changed: %+v, %v", host, err)
			}
			got, err := NewDiscoveryBindingRepo(db).List()
			if err != nil || !reflect.DeepEqual(got, []domain.DiscoveryBinding{binding}) {
				t.Fatalf("bindings changed: %+v, %v", got, err)
			}
		})
	}
}

func TestLegacySnapshotClearsDiscoveryBindings(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			db := newTestDB(t)
			id, err := NewHostRepo(db).Create(domain.Host{Name: "old", Type: "vm"})
			if err != nil {
				t.Fatal(err)
			}
			if err := NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: "proxmox:lab", ExternalID: "qemu/100", EntityType: "host", EntityID: id}); err != nil {
				t.Fatal(err)
			}
			snapshot := Snapshot{Version: version, HostTypes: domain.DefaultHostTypes(), Hosts: []domain.Host{{ID: id, Name: "restored", Type: "vm"}}}
			if err := Import(db, snapshot); err != nil {
				t.Fatal(err)
			}
			got, err := NewDiscoveryBindingRepo(db).List()
			if err != nil || len(got) != 0 {
				t.Fatalf("old identities attached to replacement inventory: %+v, %v", got, err)
			}
		})
	}
}

func TestDiscoveryBindingReplaceIsAtomicAndSourceScoped(t *testing.T) {
	db := newTestDB(t)
	id, err := NewServiceRepo(db).Create(domain.Service{Name: "manual", Kind: "container", Notes: "Preserve"})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewDiscoveryBindingRepo(db)
	old := domain.DiscoveryBinding{Source: "podman:lab", ExternalID: "old", EntityType: "service", EntityID: id}
	other := old
	other.Source = "podman:other"
	taken := old
	taken.ExternalID = "taken"
	for _, b := range []domain.DiscoveryBinding{old, other, taken} {
		if err := repo.Create(b); err != nil {
			t.Fatal(err)
		}
	}
	observations := NewDiscoveryObservationRepo(db)
	if err := observations.RecordBound(old.Source, []string{old.ExternalID}, time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace(old, "taken"); err == nil {
		t.Fatal("accepted occupied identity")
	}
	seen, err := observations.ListSource(old.Source)
	if err != nil || seen[old.ExternalID] == "" {
		t.Fatalf("conflicting replacement removed observation: %v %v", seen, err)
	}
	got, _ := repo.List()
	if len(got) != 3 {
		t.Fatal("conflict lost bindings")
	}
	stale := old
	stale.EntityID++
	if err := repo.Replace(stale, "new"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale replace: %v", err)
	}
	if err := repo.Replace(old, "new"); err != nil {
		t.Fatal(err)
	}
	seen, err = observations.ListSource(old.Source)
	if err != nil || len(seen) != 0 {
		t.Fatalf("replacement inherited old observation: %v %v", seen, err)
	}
	if err := repo.Replace(old, "newer"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeat accepted: %v", err)
	}
	got, err = repo.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.DiscoveryBinding{old, taken, other}
	want[0].ExternalID = "new"
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wrong identity replacements: %+v", got)
	}
	service, err := NewServiceRepo(db).Get(id)
	if err != nil || service.Name != "manual" || service.Notes != "Preserve" {
		t.Fatalf("record altered: %+v %v", service, err)
	}
	sentinel := errors.New("rollback")
	err = WithTx(db, func(tx *sql.Tx) error {
		if e := repo.WithTx(tx).Replace(want[0], "rolled-back"); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	again, _ := repo.List()
	if !reflect.DeepEqual(got, again) {
		t.Fatal("replacement survived rollback")
	}
}
