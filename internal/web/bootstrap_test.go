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

// TestBootstrapResetRestoresAdminRole: lockout recovery must work even when
// the last admin was demoted, so the reset also restores the admin role.
func TestBootstrapResetRestoresAdminRole(t *testing.T) {
	users := bootstrapRepo(t)
	_ = BootstrapAdmin(users, testLogger(), "admin", "password123", false)
	u, _ := users.GetByUsername("admin")
	if err := users.UpdateRole(u.ID, domain.RoleViewer, "t"); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if err := BootstrapAdmin(users, testLogger(), "admin", "newpassword", true); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if after, _ := users.GetByUsername("admin"); after.Role != domain.RoleAdmin {
		t.Fatalf("role after reset = %q, want admin", after.Role)
	}
}

// TestBootstrapResetFallsBackToOldestAdmin: when the named user does not
// exist, the reset targets the oldest admin, not whoever sorts first by name.
func TestBootstrapResetFallsBackToOldestAdmin(t *testing.T) {
	users := bootstrapRepo(t)
	_ = BootstrapAdmin(users, testLogger(), "zed", "password123", false) // oldest, admin
	if _, err := users.Create(domain.User{Username: "amy", Role: domain.RoleViewer, PasswordHash: "x", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatalf("create amy: %v", err)
	}
	if err := BootstrapAdmin(users, testLogger(), "nobody", "newpassword", true); err != nil {
		t.Fatalf("reset: %v", err)
	}
	zed, _ := users.GetByUsername("zed")
	amy, _ := users.GetByUsername("amy")
	if !verifyPassword(zed.PasswordHash, "newpassword") {
		t.Error("reset did not target the oldest admin")
	}
	if amy.Role != domain.RoleViewer || verifyPassword(amy.PasswordHash, "newpassword") {
		t.Error("reset touched a non-admin that only sorts first by name")
	}
}
