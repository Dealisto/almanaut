package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newAuthRepos(t *testing.T) (*UserRepo, *SessionRepo) {
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
	return NewUserRepo(db), NewSessionRepo(db)
}

func TestSessionUserByToken(t *testing.T) {
	users, sessions := newAuthRepos(t)
	uid, _ := users.Create(domain.User{Username: "admin", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	if _, err := sessions.Create(Session{TokenHash: "abc", UserID: uid, CreatedAt: "2026-01-01T00:00:00Z", ExpiresAt: "2999-01-01T00:00:00Z"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	u, err := sessions.UserByToken("abc", "2026-07-06T00:00:00Z")
	if err != nil {
		t.Fatalf("UserByToken: %v", err)
	}
	if u.ID != uid || u.Username != "admin" {
		t.Fatalf("got %+v", u)
	}
}

func TestSessionUserByTokenExpired(t *testing.T) {
	users, sessions := newAuthRepos(t)
	uid, _ := users.Create(domain.User{Username: "admin", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	_, _ = sessions.Create(Session{TokenHash: "abc", UserID: uid, CreatedAt: "2026-01-01T00:00:00Z", ExpiresAt: "2026-01-02T00:00:00Z"})
	if _, err := sessions.UserByToken("abc", "2026-07-06T00:00:00Z"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session: err = %v, want ErrNotFound", err)
	}
}

func TestSessionDeleteByTokenAndCascade(t *testing.T) {
	users, sessions := newAuthRepos(t)
	uid, _ := users.Create(domain.User{Username: "admin", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	_, _ = sessions.Create(Session{TokenHash: "keep", UserID: uid, CreatedAt: "t", ExpiresAt: "2999-01-01T00:00:00Z"})
	_, _ = sessions.Create(Session{TokenHash: "drop", UserID: uid, CreatedAt: "t", ExpiresAt: "2999-01-01T00:00:00Z"})

	if err := sessions.DeleteByToken("drop"); err != nil {
		t.Fatalf("DeleteByToken: %v", err)
	}
	if _, err := sessions.UserByToken("drop", "2026-07-06T00:00:00Z"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted session still resolves: %v", err)
	}
	// Deleting the user cascades to its remaining sessions.
	if err := users.Delete(uid); err != nil {
		t.Fatalf("Delete user: %v", err)
	}
	if _, err := sessions.UserByToken("keep", "2026-07-06T00:00:00Z"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session survived user delete (cascade broken): %v", err)
	}
}

func TestSessionDeleteExpired(t *testing.T) {
	users, sessions := newAuthRepos(t)
	uid, _ := users.Create(domain.User{Username: "admin", PasswordHash: "h", CreatedAt: "t", UpdatedAt: "t"})
	_, _ = sessions.Create(Session{TokenHash: "old", UserID: uid, CreatedAt: "t", ExpiresAt: "2026-01-01T00:00:00Z"})
	_, _ = sessions.Create(Session{TokenHash: "new", UserID: uid, CreatedAt: "t", ExpiresAt: "2999-01-01T00:00:00Z"})
	if err := sessions.DeleteExpired("2026-07-06T00:00:00Z"); err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if _, err := sessions.UserByToken("new", "2026-07-06T00:00:00Z"); err != nil {
		t.Fatalf("non-expired session pruned: %v", err)
	}
	if _, err := sessions.UserByToken("old", "2026-07-06T00:00:00Z"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session not pruned: %v", err)
	}
}
