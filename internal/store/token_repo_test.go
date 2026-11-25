package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newTokenRepos(t *testing.T) (*UserRepo, *TokenRepo) {
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
	return NewUserRepo(db), NewTokenRepo(db)
}

func TestTokenUserByToken(t *testing.T) {
	users, tokens := newTokenRepos(t)
	uid, _ := users.Create(domain.User{Username: "alice", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	if _, err := tokens.Create(APIToken{TokenHash: "abc", UserID: uid, Label: "ci", CreatedAt: "t"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	u, _, err := tokens.UserByToken("abc")
	if err != nil {
		t.Fatalf("UserByToken: %v", err)
	}
	if u.ID != uid || u.Username != "alice" {
		t.Fatalf("got %+v", u)
	}
	if _, _, err := tokens.UserByToken("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown token: err = %v, want ErrNotFound", err)
	}
}

func TestTokenScopeRoundTripAndBackfill(t *testing.T) {
	users, tokens := newTokenRepos(t)
	uid, _ := users.Create(domain.User{Username: "alice", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	// Explicit read-only token round-trips.
	if _, err := tokens.Create(APIToken{TokenHash: "ro", UserID: uid, Label: "l", Scope: "read-only", CreatedAt: "t"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, scope, err := tokens.UserByToken("ro")
	if err != nil {
		t.Fatalf("UserByToken: %v", err)
	}
	if scope != "read-only" {
		t.Fatalf("scope = %q, want read-only", scope)
	}
}

// TestTokenScopeColumnBackfillsReadWrite mirrors migrate_test.go's role backfill
// check: a token row inserted the way a pre-RBAC caller would (no scope column)
// must read back as read-write, so upgrading never silently revokes an
// existing API client's write access.
func TestTokenScopeColumnBackfillsReadWrite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO users (username, role, password_hash, created_at, updated_at) VALUES ('alice','admin','h','t','t')`,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO api_tokens (token_hash, user_id, label, created_at) VALUES ('legacy', 1, 'l', 't')`,
	); err != nil {
		t.Fatalf("insert legacy token: %v", err)
	}
	var scope string
	if err := db.QueryRow(`SELECT scope FROM api_tokens WHERE token_hash='legacy'`).Scan(&scope); err != nil {
		t.Fatalf("scan scope: %v", err)
	}
	if scope != "read-write" {
		t.Fatalf("legacy scope = %q, want read-write (no silent write revocation)", scope)
	}
}

func TestTokenListByUser(t *testing.T) {
	users, tokens := newTokenRepos(t)
	uid, _ := users.Create(domain.User{Username: "alice", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	other, _ := users.Create(domain.User{Username: "bob", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	_, _ = tokens.Create(APIToken{TokenHash: "a1", UserID: uid, Label: "one", CreatedAt: "t"})
	_, _ = tokens.Create(APIToken{TokenHash: "a2", UserID: uid, Label: "two", CreatedAt: "t"})
	_, _ = tokens.Create(APIToken{TokenHash: "b1", UserID: other, Label: "bob", CreatedAt: "t"})
	list, err := tokens.ListByUser(uid)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListByUser = %+v, err %v, want 2 (only alice's)", list, err)
	}
}

func TestTokenDeleteOwnerScoped(t *testing.T) {
	users, tokens := newTokenRepos(t)
	alice, _ := users.Create(domain.User{Username: "alice", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	bob, _ := users.Create(domain.User{Username: "bob", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	id, _ := tokens.Create(APIToken{TokenHash: "a1", UserID: alice, Label: "one", CreatedAt: "t"})
	// Bob cannot delete Alice's token.
	if err := tokens.Delete(id, bob); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Delete = %v, want ErrNotFound", err)
	}
	if _, _, err := tokens.UserByToken("a1"); err != nil {
		t.Fatalf("token wrongly removed by cross-user delete: %v", err)
	}
	// Alice can.
	if err := tokens.Delete(id, alice); err != nil {
		t.Fatalf("owner Delete: %v", err)
	}
	if _, _, err := tokens.UserByToken("a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token survived owner delete: %v", err)
	}
}

func TestTokenCascadeOnUserDelete(t *testing.T) {
	users, tokens := newTokenRepos(t)
	uid, _ := users.Create(domain.User{Username: "alice", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	_, _ = tokens.Create(APIToken{TokenHash: "a1", UserID: uid, Label: "one", CreatedAt: "t"})
	if err := users.Delete(uid); err != nil {
		t.Fatalf("Delete user: %v", err)
	}
	if _, _, err := tokens.UserByToken("a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token survived user delete (cascade broken): %v", err)
	}
}

func TestTokenExpiryAndLastUsedWriteBudget(t *testing.T) {
	users, tokens := newTokenRepos(t)
	uid, _ := users.Create(domain.User{Username: "alice", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	id, err := tokens.Create(APIToken{TokenHash: "timed", UserID: uid, Label: "expiring", Scope: "read-only", CreatedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tokens.userByTokenAt("timed", now); err != nil {
		t.Fatal(err)
	}
	got, err := tokens.GetByOwner(id, uid)
	if err != nil || got.LastUsedAt != now.Format(time.RFC3339) {
		t.Fatalf("first use = %+v, err=%v", got, err)
	}
	if _, err := tokens.db.Exec(`CREATE TRIGGER fail_usage BEFORE UPDATE ON api_tokens BEGIN SELECT RAISE(ABORT, 'unexpected usage write'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tokens.userByTokenAt("timed", now.Add(4*time.Minute)); err != nil {
		t.Fatalf("write inside usage window: %v", err)
	}
	if _, err := tokens.db.Exec(`DROP TRIGGER fail_usage`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tokens.userByTokenAt("timed", now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err = tokens.GetByOwner(id, uid)
	if err != nil || got.LastUsedAt != now.Add(5*time.Minute).Format(time.RFC3339) {
		t.Fatalf("refreshed use = %+v, err=%v", got, err)
	}
	for _, at := range []time.Time{now.Add(time.Hour), now.Add(2 * time.Hour)} {
		if _, _, err := tokens.userByTokenAt("timed", at); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expired token accepted at %v: %v", at, err)
		}
	}
	if _, err := tokens.GetByOwner(id, uid+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner lookup: %v", err)
	}
}

func TestTokenLifecycleLegacyDefaults(t *testing.T) {
	users, tokens := newTokenRepos(t)
	uid, _ := users.Create(domain.User{Username: "alice", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	if _, err := tokens.db.Exec(`INSERT INTO api_tokens (token_hash,user_id,label,scope,created_at) VALUES ('legacy',?,'legacy','read-only','t')`, uid); err != nil {
		t.Fatal(err)
	}
	list, err := tokens.ListByUser(uid)
	if err != nil || len(list) != 1 || list[0].ExpiresAt != "" || list[0].LastUsedAt != "" || list[0].RotatedFromID != 0 {
		t.Fatalf("legacy token=%+v err=%v", list, err)
	}
	if _, _, err := tokens.UserByToken("legacy"); err != nil {
		t.Fatal(err)
	}
}
