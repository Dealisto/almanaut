package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newTOTPRepos(t *testing.T) (*UserRepo, *TOTPRepo) {
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
	return NewUserRepo(db), NewTOTPRepo(db)
}

func seedTOTPUser(t *testing.T, users *UserRepo) int64 {
	t.Helper()
	id, err := users.Create(domain.User{Username: "u", PasswordHash: "h", Role: domain.RoleEditor, CreatedAt: "t", UpdatedAt: "t"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

func TestTOTPSecretLifecycle(t *testing.T) {
	users, totp := newTOTPRepos(t)
	uid := seedTOTPUser(t, users)

	if _, err := totp.Get(uid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get before setup = %v, want ErrNotFound", err)
	}
	if err := totp.SetSecret(uid, "SECRET", "t"); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	got, err := totp.Get(uid)
	if err != nil || got.Secret != "SECRET" || got.Enabled {
		t.Fatalf("Get = %+v, err %v; want pending secret", got, err)
	}
	if err := totp.Enable(uid); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if got, _ := totp.Get(uid); !got.Enabled {
		t.Error("expected enabled after Enable")
	}
	if err := totp.Disable(uid); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if _, err := totp.Get(uid); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after disable = %v, want ErrNotFound", err)
	}
}

func TestTOTPRecoveryCodesSingleUse(t *testing.T) {
	users, totp := newTOTPRepos(t)
	uid := seedTOTPUser(t, users)
	if err := totp.ReplaceRecoveryCodes(uid, []string{"h1", "h2", "h3"}); err != nil {
		t.Fatalf("ReplaceRecoveryCodes: %v", err)
	}
	if n, _ := totp.CountUnusedRecovery(uid); n != 3 {
		t.Fatalf("unused = %d, want 3", n)
	}
	// Redeem one; it works once.
	ok, err := totp.UseRecoveryCode(uid, "h2")
	if err != nil || !ok {
		t.Fatalf("first use = %v,%v; want true", ok, err)
	}
	if ok, _ := totp.UseRecoveryCode(uid, "h2"); ok {
		t.Error("second use of same code should fail")
	}
	if ok, _ := totp.UseRecoveryCode(uid, "nope"); ok {
		t.Error("unknown code should not redeem")
	}
	if n, _ := totp.CountUnusedRecovery(uid); n != 2 {
		t.Errorf("unused after one use = %d, want 2", n)
	}
	// Replacing resets the set.
	totp.ReplaceRecoveryCodes(uid, []string{"x1"})
	if n, _ := totp.CountUnusedRecovery(uid); n != 1 {
		t.Errorf("unused after replace = %d, want 1", n)
	}
}

func TestTOTPPending(t *testing.T) {
	users, totp := newTOTPRepos(t)
	uid := seedTOTPUser(t, users)
	if err := totp.CreatePending("tok", uid, "2026-07-10T12:00:00Z"); err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	// Valid before expiry.
	got, err := totp.PendingUser("tok", "2026-07-10T11:59:00Z")
	if err != nil || got != uid {
		t.Fatalf("PendingUser = %d,%v; want %d", got, err, uid)
	}
	// Expired.
	if _, err := totp.PendingUser("tok", "2026-07-10T12:01:00Z"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired PendingUser = %v, want ErrNotFound", err)
	}
	// Delete.
	if err := totp.DeletePending("tok"); err != nil {
		t.Fatalf("DeletePending: %v", err)
	}
	if _, err := totp.PendingUser("tok", "2026-07-10T11:59:00Z"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete = %v, want ErrNotFound", err)
	}
}
