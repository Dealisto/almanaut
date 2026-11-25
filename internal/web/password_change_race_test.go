package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/store"
)

func TestPasswordChangeRejectsStaleAuthentication(t *testing.T) {
	_, db := totpTestHandler(t)
	users, sessions, totp := store.NewUserRepo(db), store.NewSessionRepo(db), store.NewTOTPRepo(db)
	stale, err := users.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	resetHash, err := hashPassword("admin-reset-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := setPasswordAndRevoke(db, users, sessions, httptest.NewRequest(http.MethodPost, "/", nil), 1, resetHash); err != nil {
		t.Fatal(err)
	}
	// Credentials issued after the reset must survive the rejected stale write.
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if _, err := sessions.Create(store.Session{TokenHash: "current-session", UserID: 1, CreatedAt: nowRFC3339(), ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	if err := totp.CreatePending("current-challenge", 1, expires); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader("current_password=password123&new_password=stale-replacement"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), stale))
	rec := httptest.NewRecorder()
	changePassword(users, sessions, db, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), errPasswordChanged.Error()) {
		t.Fatalf("stale request = %d: %s", rec.Code, rec.Body)
	}
	got, err := users.Get(1)
	if err != nil || got.PasswordHash != resetHash {
		t.Fatalf("reset password overwritten: user=%+v err=%v", got, err)
	}
	if _, err := sessions.UserByToken("current-session", nowRFC3339()); err != nil {
		t.Fatalf("rejected write revoked a current session: %v", err)
	}
	if _, err := totp.PendingUser("current-challenge", nowRFC3339()); err != nil {
		t.Fatalf("rejected write revoked a current challenge: %v", err)
	}
}

func TestPasswordChangeRechecksAfterVerification(t *testing.T) {
	_, db := totpTestHandler(t)
	users, sessions := store.NewUserRepo(db), store.NewSessionRepo(db)
	verified, err := users.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(verified.PasswordHash, "password123") {
		t.Fatal("initial password did not verify")
	}
	request := httptest.NewRequest(http.MethodPost, "/account/password", nil)
	// The administrator wins the write lock after bcrypt has completed.
	if err := setPasswordAndRevoke(db, users, sessions, request, 1, "new-reset-hash"); err != nil {
		t.Fatal(err)
	}
	if err := changeVerifiedPassword(db, users, sessions, request, verified, "stale-hash"); !errors.Is(err, errPasswordChanged) {
		t.Fatalf("stale verification = %v", err)
	}
	got, err := users.Get(1)
	if err != nil || got.PasswordHash != "new-reset-hash" {
		t.Fatalf("reset password overwritten: user=%+v err=%v", got, err)
	}
}

func TestPasswordChangeRejectsDeletedAccount(t *testing.T) {
	_, db := totpTestHandler(t)
	users, sessions := store.NewUserRepo(db), store.NewSessionRepo(db)
	verified, err := users.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := users.Delete(1); err != nil {
		t.Fatal(err)
	}
	err = changeVerifiedPassword(db, users, sessions, httptest.NewRequest(http.MethodPost, "/", nil), verified, "replacement")
	if !errors.Is(err, errPasswordChanged) {
		t.Fatalf("deleted account = %v", err)
	}
}
