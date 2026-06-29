package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newHardwareRepo(t *testing.T) *HardwareRepo {
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
	return NewHardwareRepo(db)
}

func TestHardwareRepoCRUD(t *testing.T) {
	repo := newHardwareRepo(t)

	id, err := repo.Create(domain.Hardware{
		Name: "APC Smart-UPS", Kind: "ups", Manufacturer: "APC",
		Model: "SMT1500", Serial: "AS123", Location: "rack-1",
		PurchaseDate: "2024-03-01", WarrantyEnd: "2027-03-01", Status: "active",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "APC Smart-UPS" || got.Kind != "ups" || got.WarrantyEnd != "2027-03-01" {
		t.Errorf("Get returned %+v", got)
	}

	updated := domain.Hardware{
		ID: id, Name: "APC Smart-UPS", Kind: "ups", Manufacturer: "APC",
		Model: "SMT1500RM", Serial: "AS999", Location: "rack-2",
		PurchaseDate: "2024-03-01", WarrantyEnd: "2028-03-01", Status: "spare",
		Notes: "moved to rack 2",
	}
	if err := repo.Update(updated); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = repo.Get(id)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got != updated {
		t.Errorf("Update did not persist all fields:\n got  %+v\n want %+v", got, updated)
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
	list, err = repo.List()
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List len after delete = %d, want 0", len(list))
	}
}
