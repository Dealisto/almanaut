package web

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func bootstrapRepo(t *testing.T) (*sql.DB, *store.UserRepo) {
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
	return db, store.NewUserRepo(db)
}

func TestBootstrapSeedsAdminFromEnv(t *testing.T) {
	db, users := bootstrapRepo(t)
	if err := BootstrapAdmin(db, testLogger(), "root", "password123", false); err != nil {
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
	db, users := bootstrapRepo(t)
	if err := BootstrapAdmin(db, testLogger(), "", "", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	if _, err := users.GetByUsername("admin"); err != nil {
		t.Fatalf("default admin not created: %v", err)
	}
}

func TestBootstrapIdempotentWhenUsersExist(t *testing.T) {
	db, users := bootstrapRepo(t)
	_ = BootstrapAdmin(db, testLogger(), "admin", "password123", false)
	u1, _ := users.GetByUsername("admin")
	// Second call with users present and reset=false must not change anything.
	_ = BootstrapAdmin(db, testLogger(), "admin", "different", false)
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
	if err := BootstrapAdmin(db, testLogger(), "root", "password123", false); err != nil {
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
	db, users := bootstrapRepo(t)
	_ = BootstrapAdmin(db, testLogger(), "admin", "password123", false)
	before, _ := users.GetByUsername("admin")
	if err := BootstrapAdmin(db, testLogger(), "admin", "newpassword", true); err != nil {
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
	db, users := bootstrapRepo(t)
	_ = BootstrapAdmin(db, testLogger(), "admin", "password123", false)
	u, _ := users.GetByUsername("admin")
	if err := users.UpdateRole(u.ID, domain.RoleViewer, "t"); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if err := BootstrapAdmin(db, testLogger(), "admin", "newpassword", true); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if after, _ := users.GetByUsername("admin"); after.Role != domain.RoleAdmin {
		t.Fatalf("role after reset = %q, want admin", after.Role)
	}
}

// TestBootstrapResetFallsBackToOldestAdmin: when the named user does not
// exist, the reset targets the oldest admin, not whoever sorts first by name.
func TestBootstrapResetFallsBackToOldestAdmin(t *testing.T) {
	db, users := bootstrapRepo(t)
	_ = BootstrapAdmin(db, testLogger(), "zed", "password123", false) // oldest, admin
	if _, err := users.Create(domain.User{Username: "amy", Role: domain.RoleViewer, PasswordHash: "x", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatalf("create amy: %v", err)
	}
	if err := BootstrapAdmin(db, testLogger(), "nobody", "newpassword", true); err != nil {
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

func TestBootstrapResetRevokesAuthenticationStateAtomically(t *testing.T) {
	for _, tc := range []struct {
		name, trigger string
	}{
		{"success", ""},
		{"session deletion failure", "BEFORE DELETE ON sessions"},
		{"challenge deletion failure", "BEFORE DELETE ON totp_pending"},
		{"promotion failure", "BEFORE UPDATE OF role ON users"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, users := bootstrapRepo(t)
			if err := BootstrapAdmin(db, testLogger(), "admin", "password123", false); err != nil {
				t.Fatal(err)
			}
			if err := users.UpdateRole(1, domain.RoleViewer, "t"); err != nil {
				t.Fatal(err)
			}
			before, err := users.Get(1)
			if err != nil {
				t.Fatal(err)
			}
			other, err := users.Create(domain.User{Username: "other", Role: domain.RoleEditor, PasswordHash: "hash", CreatedAt: "t", UpdatedAt: "t"})
			if err != nil {
				t.Fatal(err)
			}
			totp := store.NewTOTPRepo(db)
			for _, uid := range []int64{1, other} {
				key := strconv.FormatInt(uid, 10)
				if _, err := store.NewSessionRepo(db).Create(store.Session{UserID: uid, TokenHash: "session-" + key, CreatedAt: "t", ExpiresAt: "2099-01-01T00:00:00Z"}); err != nil {
					t.Fatal(err)
				}
				if err := totp.CreatePending("pending-"+key, uid, "2099-01-01T00:00:00Z"); err != nil {
					t.Fatal(err)
				}
			}
			if err := totp.SetSecret(1, "UNCHANGED", "t"); err != nil {
				t.Fatal(err)
			}
			if err := totp.Enable(1); err != nil {
				t.Fatal(err)
			}
			if err := totp.ReplaceRecoveryCodes(1, []string{"recovery-hash"}); err != nil {
				t.Fatal(err)
			}
			tokens := store.NewTokenRepo(db)
			if _, err := tokens.Create(store.APIToken{TokenHash: "api-hash", UserID: 1, Label: "keep", Scope: string(domain.ScopeReadWrite), CreatedAt: "t"}); err != nil {
				t.Fatal(err)
			}
			if tc.trigger != "" {
				if _, err := db.Exec("CREATE TRIGGER fail_reset " + tc.trigger + " BEGIN SELECT RAISE(ABORT, 'injected failure'); END"); err != nil {
					t.Fatal(err)
				}
			}
			err = BootstrapAdmin(db, testLogger(), "admin", "newpassword123", true)
			if (err != nil) != (tc.trigger != "") {
				t.Fatalf("reset error = %v, injected failure = %v", err, tc.trigger != "")
			}
			after, err := users.Get(1)
			if err != nil {
				t.Fatal(err)
			}
			if tc.trigger != "" {
				if after.PasswordHash != before.PasswordHash || after.Role != before.Role {
					t.Error("failed reset changed password or role")
				}
			} else if !verifyPassword(after.PasswordHash, "newpassword123") || after.Role != domain.RoleAdmin {
				t.Error("successful reset did not replace password and restore admin")
			}
			for _, table := range []string{"sessions", "totp_pending"} {
				for _, uid := range []int64{1, other} {
					want := 1
					if uid == 1 && tc.trigger == "" {
						want = 0
					}
					var count int
					if err := db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE user_id=?", uid).Scan(&count); err != nil || count != want {
						t.Errorf("%s for user %d = %d, want %d; err=%v", table, uid, count, want, err)
					}
				}
			}
			factor, err := totp.Get(1)
			if err != nil || !factor.Enabled || factor.Secret != "UNCHANGED" {
				t.Errorf("reset changed enrolled factor: %+v err=%v", factor, err)
			}
			if count, err := totp.CountUnusedRecovery(1); err != nil || count != 1 {
				t.Errorf("reset changed recovery codes: count=%d err=%v", count, err)
			}
			if _, _, err := tokens.UserByToken("api-hash"); err != nil {
				t.Errorf("reset revoked API token: %v", err)
			}
		})
	}
}
