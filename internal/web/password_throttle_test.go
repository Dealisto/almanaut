package web

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestPasswordChangeLimitsGuessesAcrossSessions(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	otherUser := seedUserAndLogin(t, h, db, "other", domain.RoleViewer)
	for range loginFailureThreshold {
		rec := postCSRF(t, h, session, "/account/password", "current_password=wrong&new_password=replacement123")
		if !strings.Contains(rec.Body.String(), "current password is incorrect") {
			t.Fatalf("wrong password response: %d %s", rec.Code, rec.Body)
		}
	}
	// A fresh successful login must not reset the separate password-change
	// budget, and switching sessions must not create another guessing budget.
	fresh := loginAs(t, h, "admin", "password123")
	for _, cookie := range []*http.Cookie{session, fresh} {
		rec := postCSRF(t, h, cookie, "/account/password", "current_password=password123&new_password=replacement123")
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("locked password change: %d %s", rec.Code, rec.Body)
		}
		if retry, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || retry <= 0 || retry > int(loginLockoutDuration.Seconds()) {
			t.Fatalf("invalid Retry-After: %q", rec.Header().Get("Retry-After"))
		}
	}
	u, err := store.NewUserRepo(db).Get(1)
	if err != nil || !verifyPassword(u.PasswordHash, "password123") {
		t.Fatalf("throttled request changed the password: %v", err)
	}
	if !sessionAlive(t, h, session) || !sessionAlive(t, h, fresh) {
		t.Fatal("throttled request revoked sessions")
	}
	rec := postCSRF(t, h, otherUser, "/account/password", "current_password=password123&new_password=replacement123")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "password updated") {
		t.Fatalf("another user's budget was affected: %d %s", rec.Code, rec.Body)
	}
}

func TestSuccessfulPasswordChangeClearsItsFailureBudget(t *testing.T) {
	h, session := authTestServer(t)
	current := "password123"
	for _, next := range []string{"replacement123", "anotherpassword123"} {
		for range loginFailureThreshold - 1 {
			postCSRF(t, h, session, "/account/password", "current_password=wrong&new_password="+next)
		}
		rec := postCSRF(t, h, session, "/account/password", "current_password="+current+"&new_password="+next)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "password updated") {
			t.Fatalf("successful change failed to clear budget: %d %s", rec.Code, rec.Body)
		}
		current = next
	}
}

func TestPasswordVerificationRejectsTruncatedSuffix(t *testing.T) {
	password := strings.Repeat("p", domain.MaxPasswordBytes)
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(hash, password) {
		t.Fatal("maximum-length password rejected")
	}
	if verifyPassword(hash, password+"different") {
		t.Fatal("oversized password accepted after bcrypt silently ignored its suffix")
	}
}
