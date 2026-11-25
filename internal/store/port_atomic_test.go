package store

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestDirectPortWritesRollbackOnConnectionFailure(t *testing.T) {
	db := newTestDB(t)
	r := NewPortRepo(db)
	if _, err := r.Create(domain.Port{OwnerType: "host", OwnerID: 1, Name: "bad", PeerPortID: 999}); err == nil {
		t.Fatal("missing peer accepted")
	}
	if n, err := r.Count(); err != nil || n != 0 {
		t.Fatalf("failed create left rows: %d, %v", n, err)
	}
	a, err := r.Create(domain.Port{OwnerType: "host", OwnerID: 1, Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Create(domain.Port{OwnerType: "host", OwnerID: 2, Name: "b", PeerPortID: a})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Update(domain.Port{ID: a, OwnerType: "host", OwnerID: 1, Name: "changed", PeerPortID: 999}); err == nil {
		t.Fatal("missing peer accepted during update")
	}
	got, err := r.Get(a)
	if err != nil || got.Name != "a" || got.PeerPortID != b {
		t.Fatalf("failed update was partially applied: %+v, %v", got, err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_port_delete BEFORE DELETE ON ports BEGIN SELECT RAISE(ABORT, 'delete failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(a); err == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	got, err = r.Get(b)
	if err != nil || got.PeerPortID != a {
		t.Fatalf("failed delete disconnected surviving peer: %+v, %v", got, err)
	}
}

func TestDirectNICWritesRollbackWhenPortCleanupFails(t *testing.T) {
	db := newTestDB(t)
	nics, ports := NewNICRepo(db), NewPortRepo(db)
	id, err := nics.Create(domain.NIC{HostID: 1, Name: "onboard", Kind: "onboard"})
	if err != nil {
		t.Fatal(err)
	}
	portID, err := ports.Create(domain.Port{OwnerType: "host", OwnerID: 1, NICID: id, Name: "eth0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_nic_cleanup BEFORE UPDATE OF nic_id ON ports BEGIN SELECT RAISE(ABORT, 'cleanup failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := nics.Update(domain.NIC{ID: id, HostID: 2, Name: "moved", Kind: "onboard"}); err == nil {
		t.Fatal("move unexpectedly succeeded")
	}
	got, err := nics.Get(id)
	if err != nil || got.HostID != 1 || got.Name != "onboard" {
		t.Fatalf("failed move changed NIC: %+v, %v", got, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_nic_cleanup`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_nic_delete BEFORE DELETE ON nics BEGIN SELECT RAISE(ABORT, 'delete failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := nics.Delete(id); err == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	port, err := ports.Get(portID)
	if err != nil || port.NICID != id {
		t.Fatalf("failed delete cleared attribution: %+v, %v", port, err)
	}
}
