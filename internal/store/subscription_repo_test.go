package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newSubscriptionRepo(t *testing.T) *SubscriptionRepo {
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
	return NewSubscriptionRepo(db)
}

func TestSubscriptionRepoCRUD(t *testing.T) {
	repo := newSubscriptionRepo(t)

	id, err := repo.Create(domain.Subscription{
		Name: "Hetzner VPS", Kind: "vps", Provider: "Hetzner",
		Amount: "12.99", Currency: "EUR", BillingCycle: "monthly",
		RenewalDate: "2027-01-15", AutoRenew: true, Status: "active",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Hetzner VPS" || got.Amount != "12.99" || !got.AutoRenew {
		t.Errorf("Get returned %+v", got)
	}

	updated := domain.Subscription{
		ID: id, Name: "Hetzner VPS", Kind: "vps", Provider: "Hetzner",
		Amount: "14.99", Currency: "EUR", BillingCycle: "monthly",
		RenewalDate: "2028-01-15", AutoRenew: false, Status: "active",
		Notes: "upgraded plan",
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
