package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newUserRepo(t *testing.T) *UserRepo {
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
	return NewUserRepo(db)
}

func TestUserRepoCreateGetByUsername(t *testing.T) {
	r := newUserRepo(t)
	id, err := r.Create(domain.User{Username: "admin", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := r.GetByUsername("admin")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if got.ID != id || got.PasswordHash != "h" {
		t.Fatalf("got %+v", got)
	}
}

func TestUserRepoGetByUsernameMissing(t *testing.T) {
	r := newUserRepo(t)
	if _, err := r.GetByUsername("nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUserRepoCountAndList(t *testing.T) {
	r := newUserRepo(t)
	if n, _ := r.Count(); n != 0 {
		t.Fatalf("empty Count = %d, want 0", n)
	}
	_, _ = r.Create(domain.User{Username: "b", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	_, _ = r.Create(domain.User{Username: "a", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	n, err := r.Count()
	if err != nil || n != 2 {
		t.Fatalf("Count = %d, err %v, want 2", n, err)
	}
	list, err := r.List()
	if err != nil || len(list) != 2 || list[0].Username != "a" {
		t.Fatalf("List = %+v, err %v (want ordered by username)", list, err)
	}
}

func TestUserRepoUpdatePassword(t *testing.T) {
	r := newUserRepo(t)
	id, _ := r.Create(domain.User{Username: "admin", PasswordHash: "old", CreatedAt: "t", UpdatedAt: "t"})
	if err := r.UpdatePassword(id, "new", "t2"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	got, _ := r.Get(id)
	if got.PasswordHash != "new" || got.UpdatedAt != "t2" {
		t.Fatalf("got %+v", got)
	}
	if err := r.UpdatePassword(9999, "x", "t"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdatePassword missing = %v, want ErrNotFound", err)
	}
}

func TestUserRepoUniqueUsername(t *testing.T) {
	r := newUserRepo(t)
	if _, err := r.Create(domain.User{Username: "admin", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := r.Create(domain.User{Username: "admin", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"}); err == nil {
		t.Fatal("duplicate username must be rejected by UNIQUE constraint")
	}
}

func TestUserRepoRoundTripsRole(t *testing.T) {
	r := newUserRepo(t)
	id, err := r.Create(domain.User{Username: "ed", Role: domain.RoleEditor, PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Role != domain.RoleEditor {
		t.Fatalf("role = %q, want editor", got.Role)
	}
}
