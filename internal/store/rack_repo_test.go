package store

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestSiteRepoRoundTrip(t *testing.T) {
	db := newTestDB(t) // existing store-test helper: opens temp DB + Migrate
	repo := NewSiteRepo(db)
	id, err := repo.Create(domainSiteFixture())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "hq" {
		t.Fatalf("name = %q, want hq", got.Name)
	}
	got.Name = "hq2"
	if err := repo.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := repo.Get(id)
	if after.Name != "hq2" {
		t.Fatalf("update not persisted: %q", after.Name)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(id); err == nil {
		t.Fatal("expected ErrNotFound after delete")
	}
}

func TestRackRepoPersistsUHeight(t *testing.T) {
	db := newTestDB(t)
	repo := NewRackRepo(db)
	id, err := repo.Create(rackFixture())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, _ := repo.Get(id)
	if got.UHeight != 24 || got.LocationID != 7 {
		t.Fatalf("rack fields not persisted: %+v", got)
	}
}

func TestHostRepoPersistsRackPlacement(t *testing.T) {
	db := newTestDB(t)
	repo := NewHostRepo(db)
	id, err := repo.Create(domain.Host{Name: "srv", Type: "physical", RackID: 4, RackPosition: 10, UHeight: 2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, _ := repo.Get(id)
	if got.RackID != 4 || got.RackPosition != 10 || got.UHeight != 2 {
		t.Fatalf("host rack placement not persisted: %+v", got)
	}
}

func TestHardwareRepoPersistsRackPlacement(t *testing.T) {
	db := newTestDB(t)
	repo := NewHardwareRepo(db)
	id, err := repo.Create(domain.Hardware{Name: "sw", RackID: 4, RackPosition: 5, UHeight: 2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, _ := repo.Get(id)
	if got.RackID != 4 || got.RackPosition != 5 || got.UHeight != 2 {
		t.Fatalf("hardware rack placement not persisted: %+v", got)
	}
}

func domainSiteFixture() domain.Site { return domain.Site{Name: "hq", Address: "1 St"} }
func rackFixture() domain.Rack       { return domain.Rack{Name: "r1", LocationID: 7, UHeight: 24} }
