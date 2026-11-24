package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestDiscoveryObservationsAreBoundScopedAndMonotonic(t *testing.T) {
	db := newTestDB(t)
	id, err := NewServiceRepo(db).Create(domain.Service{Name: "app", Kind: "container"})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"docker:a", "docker:b"} {
		if err := NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: source, ExternalID: "same", EntityType: "service", EntityID: id}); err != nil {
			t.Fatal(err)
		}
	}
	r := NewDiscoveryObservationRepo(db)
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	if err := r.RecordBound("docker:a", []string{"same", "unknown", "same"}, now); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordBound("docker:a", []string{"same"}, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordBound("docker:a", nil, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := r.ListSource("docker:a")
	if err != nil || len(got) != 1 || got["same"] != now.Format(time.RFC3339) {
		t.Fatalf("last successful observation = %v, %v", got, err)
	}
	other, err := r.ListSource("docker:b")
	if err != nil || len(other) != 0 {
		t.Fatalf("source isolation = %v, %v", other, err)
	}
	if err := NewServiceRepo(db).Delete(id); err != nil {
		t.Fatal(err)
	}
	got, err = r.ListSource("docker:a")
	if err != nil || len(got) != 0 {
		t.Fatalf("deleted binding observation retained: %v, %v", got, err)
	}
}

func TestDiscoveryObservationsRollbackAndInventoryReplacement(t *testing.T) {
	db := newTestDB(t)
	id, err := NewHostRepo(db).Create(domain.Host{Name: "vm", Type: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: "proxmox:lab", ExternalID: "qemu/1", EntityType: "host", EntityID: id}); err != nil {
		t.Fatal(err)
	}
	r := NewDiscoveryObservationRepo(db)
	rollback := errors.New("rollback")
	err = WithTx(db, func(tx *sql.Tx) error {
		if err := r.WithTx(tx).RecordBound("proxmox:lab", []string{"qemu/1"}, time.Now()); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	seen, err := r.ListSource("proxmox:lab")
	if err != nil || len(seen) != 0 {
		t.Fatalf("rollback = %v, %v", seen, err)
	}
	if err := r.RecordBound("proxmox:lab", []string{"qemu/1"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := Import(db, snapshot); err != nil {
		t.Fatal(err)
	}
	seen, err = r.ListSource("proxmox:lab")
	if err != nil || len(seen) != 0 {
		t.Fatalf("replacement retained observations: %v, %v", seen, err)
	}
	bindings, err := NewDiscoveryBindingRepo(db).List()
	if err != nil || len(bindings) != 1 {
		t.Fatalf("replacement lost inventory binding: %v, %v", bindings, err)
	}
}
