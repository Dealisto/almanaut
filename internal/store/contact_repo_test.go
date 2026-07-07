package store

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestContactRepoRoundTrip(t *testing.T) {
	db := newTestDB(t)
	repo := NewContactRepo(db)
	id, err := repo.Create(domain.Contact{Name: "Ada", Email: "ada@x.io", Role: "admin", Organization: "Acme"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Ada" || got.Email != "ada@x.io" || got.Role != "admin" || got.Organization != "Acme" {
		t.Fatalf("fields not persisted: %+v", got)
	}
	got.Role = "owner"
	if err := repo.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := repo.Get(id)
	if after.Role != "owner" {
		t.Fatalf("update not persisted: %q", after.Role)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(id); err == nil {
		t.Fatal("expected ErrNotFound after delete")
	}
}
