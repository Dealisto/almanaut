package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newNetworkRepo(t *testing.T) *NetworkRepo {
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
	return NewNetworkRepo(db)
}

func TestNetworkRepoCRUD(t *testing.T) {
	repo := newNetworkRepo(t)

	id, err := repo.Create(domain.Network{Name: "lan", CIDR: "10.0.0.0/24", Gateway: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "lan" || got.CIDR != "10.0.0.0/24" || got.Gateway != "10.0.0.1" {
		t.Errorf("Get returned %+v", got)
	}

	if err := repo.Update(domain.Network{ID: id, Name: "lan", CIDR: "10.0.0.0/24", VLANID: 10}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = repo.Get(id)
	if got.VLANID != 10 {
		t.Errorf("Update not applied: vlan_id = %d", got.VLANID)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}

	if err := repo.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, _ = repo.List()
	if len(list) != 0 {
		t.Fatalf("List len after delete = %d, want 0", len(list))
	}
}

func TestNetworkRepoCount(t *testing.T) {
	repo := newNetworkRepo(t)

	if n, err := repo.Count(); err != nil || n != 0 {
		t.Fatalf("Count on empty = (%d, %v), want (0, nil)", n, err)
	}
	for _, cidr := range []string{"10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24"} {
		if _, err := repo.Create(domain.Network{Name: cidr, CIDR: cidr}); err != nil {
			t.Fatalf("Create %s: %v", cidr, err)
		}
	}
	if n, err := repo.Count(); err != nil || n != 3 {
		t.Fatalf("Count = (%d, %v), want (3, nil)", n, err)
	}
}
