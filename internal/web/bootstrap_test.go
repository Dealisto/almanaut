package web

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func bootstrapRepo(t *testing.T) *store.UserRepo {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store.NewUserRepo(db)
}

func TestBootstrapSeedsAdminFromEnv(t *testing.T) {
	users := bootstrapRepo(t)
	if err := BootstrapAdmin(users, testLogger(), "root", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	u, err := users.GetByUsername("root")
	if err != nil {
		t.Fatalf("admin not created: %v", err)
	}
	if !verifyPassword(u.PasswordHash, "password123") {
		t.Fatal("seeded password does not verify")
	}
}

func TestBootstrapGeneratesAdminWhenNoEnv(t *testing.T) {
	users := bootstrapRepo(t)
	if err := BootstrapAdmin(users, testLogger(), "", "", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if _, err := users.GetByUsername("admin"); err != nil {
		t.Fatalf("default admin not created: %v", err)
	}
}

func TestBootstrapIdempotentWhenUsersExist(t *testing.T) {
	users := bootstrapRepo(t)
	_ = BootstrapAdmin(users, testLogger(), "admin", "password123", false)
	u1, _ := users.GetByUsername("admin")
	// Second call with users present and reset=false must not change anything.
	_ = BootstrapAdmin(users, testLogger(), "admin", "different", false)
	u2, _ := users.GetByUsername("admin")
	if u1.PasswordHash != u2.PasswordHash {
		t.Fatal("bootstrap must be a no-op when users already exist and reset is false")
	}
	if n, _ := users.Count(); n != 1 {
		t.Fatalf("Count = %d, want 1 (no duplicate admin)", n)
	}
}

func TestBootstrapAdminSeedsAdminRole(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	users := store.NewUserRepo(db)
	if err := BootstrapAdmin(users, testLogger(), "root", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	u, err := users.GetByUsername("root")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if u.Role != domain.RoleAdmin {
		t.Fatalf("bootstrap role = %q, want admin", u.Role)
	}
}

func TestBootstrapResetChangesPassword(t *testing.T) {
	users := bootstrapRepo(t)
	_ = BootstrapAdmin(users, testLogger(), "admin", "password123", false)
	before, _ := users.GetByUsername("admin")
	if err := BootstrapAdmin(users, testLogger(), "admin", "newpassword", true); err != nil {
		t.Fatalf("reset: %v", err)
	}
	after, _ := users.GetByUsername("admin")
	if before.PasswordHash == after.PasswordHash {
		t.Fatal("reset must change the password hash")
	}
	if !verifyPassword(after.PasswordHash, "newpassword") {
		t.Fatal("reset password does not verify")
	}
}
