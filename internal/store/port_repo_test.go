package store

import (
	"errors"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestPortRepoRoundTrip(t *testing.T) {
	db := newTestDB(t)
	hwID, err := NewHardwareRepo(db).Create(domain.Hardware{Name: "switch01", Kind: "switch"})
	if err != nil {
		t.Fatalf("create hardware: %v", err)
	}
	repo := NewPortRepo(db)
	id, err := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: hwID, Name: "Port 1", MAC: "aa:bb:cc:dd:ee:ff", MgmtOnly: true, Notes: "uplink"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OwnerType != "hardware" || got.OwnerID != hwID || got.Name != "Port 1" || got.MAC != "aa:bb:cc:dd:ee:ff" || !got.MgmtOnly || got.Notes != "uplink" {
		t.Fatalf("fields not persisted: %+v", got)
	}
	if got.OwnerName != "switch01" {
		t.Fatalf("OwnerName not derived: %q", got.OwnerName)
	}
	got.Name = "Port 1 renamed"
	if err := repo.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := repo.Get(id)
	if after.Name != "Port 1 renamed" {
		t.Fatalf("update not persisted: %q", after.Name)
	}
	if n, err := repo.Count(); err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestPortRepoUpdateMissingIsNotFound(t *testing.T) {
	db := newTestDB(t)
	err := NewPortRepo(db).Update(domain.Port{ID: 99, OwnerType: "host", OwnerID: 1, Name: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// The peer link is stored on one side only; both sides must still see it.
func TestPortRepoResolvesPeerBothWays(t *testing.T) {
	db := newTestDB(t)
	hostID, err := NewHostRepo(db).Create(domain.Host{Name: "server1"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	hwID, err := NewHardwareRepo(db).Create(domain.Hardware{Name: "switch01", Kind: "switch"})
	if err != nil {
		t.Fatalf("create hardware: %v", err)
	}
	nicID, err := NewNICRepo(db).Create(domain.NIC{HostID: hostID, Name: "onboard", Kind: "onboard"})
	if err != nil {
		t.Fatalf("create nic: %v", err)
	}
	repo := NewPortRepo(db)
	swPort, err := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: hwID, Name: "Port 12"})
	if err != nil {
		t.Fatalf("create switch port: %v", err)
	}
	hostPort, err := repo.Create(domain.Port{OwnerType: "host", OwnerID: hostID, NICID: nicID, Name: "eth0", PeerPortID: swPort})
	if err != nil {
		t.Fatalf("create host port: %v", err)
	}

	hp, err := repo.Get(hostPort)
	if err != nil {
		t.Fatalf("get host port: %v", err)
	}
	if hp.PeerID != swPort || hp.PeerLabel != "switch01 / Port 12" {
		t.Fatalf("forward peer not resolved: PeerID=%d PeerLabel=%q", hp.PeerID, hp.PeerLabel)
	}
	if hp.NICName != "onboard" {
		t.Fatalf("NICName not derived: %q", hp.NICName)
	}

	sp, err := repo.Get(swPort)
	if err != nil {
		t.Fatalf("get switch port: %v", err)
	}
	if sp.PeerID != hostPort || sp.PeerLabel != "server1 / eth0" {
		t.Fatalf("reverse peer not resolved: PeerID=%d PeerLabel=%q", sp.PeerID, sp.PeerLabel)
	}
}

// Deleting a port must clear dangling peer_port_id pointers on other ports.
func TestPortRepoDeleteClearsPeerPointers(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortRepo(db)
	a, err := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: 1, Name: "Port 1"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := repo.Create(domain.Port{OwnerType: "host", OwnerID: 1, Name: "eth0", PeerPortID: a})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if err := repo.Delete(a); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err := repo.Get(b)
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if got.PeerPortID != 0 || got.PeerID != 0 {
		t.Fatalf("peer pointer should be cleared, got PeerPortID=%d PeerID=%d", got.PeerPortID, got.PeerID)
	}
}

// "Port 2" must sort before "Port 10" (length-then-name natural order).
func TestPortRepoListNaturalOrder(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortRepo(db)
	for _, name := range []string{"Port 10", "Port 2", "Port 1"} {
		if _, err := repo.Create(domain.Port{OwnerType: "hardware", OwnerID: 1, Name: name}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	items, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 || items[0].Name != "Port 1" || items[1].Name != "Port 2" || items[2].Name != "Port 10" {
		names := []string{}
		for _, p := range items {
			names = append(names, p.Name)
		}
		t.Fatalf("unexpected order: %v", names)
	}
}

// A dangling owner reference is surfaced, not hidden.
func TestPortRepoMarksDeletedOwner(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortRepo(db)
	id, err := repo.Create(domain.Port{OwnerType: "host", OwnerID: 42, Name: "eth0"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OwnerName != "host:42 (deleted)" {
		t.Fatalf("dangling owner should be marked, got %q", got.OwnerName)
	}
}
