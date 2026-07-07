package store

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestVLANRepoRoundTrip(t *testing.T) {
	db := newTestDB(t)
	repo := NewVLANRepo(db)
	id, err := repo.Create(domain.VLAN{Name: "mgmt", VID: 10})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "mgmt" || got.VID != 10 {
		t.Fatalf("fields not persisted: %+v", got)
	}
	got.VID = 20
	if err := repo.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := repo.Get(id)
	if after.VID != 20 {
		t.Fatalf("update not persisted: %d", after.VID)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(id); err == nil {
		t.Fatal("expected ErrNotFound after delete")
	}
}

func TestNetworkRepoPersistsVLANID(t *testing.T) {
	db := newTestDB(t)
	repo := NewNetworkRepo(db)
	id, err := repo.Create(domain.Network{Name: "lan", CIDR: "10.0.0.0/24", VLANID: 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, _ := repo.Get(id)
	if got.VLANID != 7 {
		t.Fatalf("vlan_id not persisted: %+v", got)
	}
}
