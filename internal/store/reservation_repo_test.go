package store

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestReservationRepoRoundTrip(t *testing.T) {
	db := newTestDB(t)
	repo := NewReservationRepo(db)
	id, err := repo.Create(domain.Reservation{NetworkID: 3, Name: "dhcp", StartIP: "10.0.0.10", EndIP: "10.0.0.50"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.NetworkID != 3 || got.Name != "dhcp" || got.StartIP != "10.0.0.10" || got.EndIP != "10.0.0.50" {
		t.Fatalf("fields not persisted: %+v", got)
	}
	got.EndIP = "10.0.0.60"
	if err := repo.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := repo.Get(id)
	if after.EndIP != "10.0.0.60" {
		t.Fatalf("update not persisted: %q", after.EndIP)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(id); err == nil {
		t.Fatal("expected ErrNotFound after delete")
	}
}
