package store

import (
	"errors"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestNICRepoRoundTrip(t *testing.T) {
	db := newTestDB(t)
	hostID, err := NewHostRepo(db).Create(domain.Host{Name: "server1"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	repo := NewNICRepo(db)
	id, err := repo.Create(domain.NIC{HostID: hostID, Name: "X540-T2", Kind: "expansion", Model: "Intel X540-T2", Serial: "ABC123"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.HostID != hostID || got.Name != "X540-T2" || got.Kind != "expansion" || got.Model != "Intel X540-T2" || got.Serial != "ABC123" {
		t.Fatalf("fields not persisted: %+v", got)
	}
	if got.HostName != "server1" {
		t.Fatalf("HostName not derived: %q", got.HostName)
	}
	got.Kind = "onboard"
	if err := repo.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := repo.Get(id)
	if after.Kind != "onboard" {
		t.Fatalf("update not persisted: %q", after.Kind)
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

func TestNICRepoUpdateMissingIsNotFound(t *testing.T) {
	db := newTestDB(t)
	err := NewNICRepo(db).Update(domain.NIC{ID: 99, HostID: 1, Name: "x", Kind: "onboard"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestNICRepoDeleteClearsPortAttribution(t *testing.T) {
	db := newTestDB(t)
	nics := NewNICRepo(db)
	ports := NewPortRepo(db)
	nicID, err := nics.Create(domain.NIC{HostID: 1, Name: "onboard", Kind: "onboard"})
	if err != nil {
		t.Fatalf("create nic: %v", err)
	}
	portID, err := ports.Create(domain.Port{OwnerType: "host", OwnerID: 1, NICID: nicID, Name: "eth0"})
	if err != nil {
		t.Fatalf("create port: %v", err)
	}
	if err := nics.Delete(nicID); err != nil {
		t.Fatalf("delete nic: %v", err)
	}
	p, err := ports.Get(portID)
	if err != nil {
		t.Fatalf("get port: %v", err)
	}
	if p.NICID != 0 {
		t.Fatalf("port NICID should be cleared on NIC delete, got %d", p.NICID)
	}
}
