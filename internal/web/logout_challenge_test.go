package web

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestLogoutRevokesBrowserSessionAndPendingChallengeAtomically(t *testing.T) {
	for _, failTable := range []string{"", "sessions", "totp_pending"} {
		t.Run("failure="+failTable, func(t *testing.T) {
			h, db := totpTestHandler(t)
			current := loginAs(t, h, "admin", "password123")
			_, recovery := enroll2FA(t, h, db, current, 1)
			pending := cookieNamed(passwordLogin(t, h, "admin", "password123").Result().Cookies(), pending2FACookieName)
			if pending == nil {
				t.Fatal("missing pending challenge")
			}
			sessions, factors := store.NewSessionRepo(db), store.NewTOTPRepo(db)
			if _, err := sessions.Create(store.Session{TokenHash: hashToken("other-browser"), UserID: 1, CreatedAt: nowRFC3339(), ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); err != nil {
				t.Fatal(err)
			}
			if err := factors.CreatePending(hashToken("other-challenge"), 1, time.Now().Add(time.Minute).UTC().Format(time.RFC3339)); err != nil {
				t.Fatal(err)
			}
			browser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.AddCookie(pending)
				h.ServeHTTP(w, r)
			})
			if failTable != "" {
				if _, err := db.Exec(fmt.Sprintf("CREATE TRIGGER fail_browser_logout BEFORE DELETE ON %s BEGIN SELECT RAISE(ABORT, 'logout storage unavailable'); END", failTable)); err != nil {
					t.Fatal(err)
				}
				rec := postCSRF(t, browser, current, "/logout", "")
				if rec.Code != http.StatusInternalServerError {
					t.Fatalf("failed logout status=%d", rec.Code)
				}
				for _, name := range []string{sessionCookieName, pending2FACookieName} {
					if cookieNamed(rec.Result().Cookies(), name) != nil {
						t.Fatalf("failed logout cleared %s needed for retry", name)
					}
				}
				if _, err := sessions.UserByToken(hashToken(current.Value), nowRFC3339()); err != nil {
					t.Fatalf("failed logout partially revoked the session: %v", err)
				}
				if _, err := factors.PendingUser(hashToken(pending.Value), nowRFC3339()); err != nil {
					t.Fatalf("failed logout partially revoked the challenge: %v", err)
				}
				if events, err := store.NewAuthEventRepo(db).List(store.AuthEventFilter{Type: domain.AuthLogout}); err != nil || len(events) != 0 {
					t.Fatalf("failed logout recorded success: %+v %v", events, err)
				}
				if _, err := db.Exec("DROP TRIGGER fail_browser_logout"); err != nil {
					t.Fatal(err)
				}
			}
			rec := postCSRF(t, browser, current, "/logout", "")
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
				t.Fatalf("logout status=%d location=%q", rec.Code, rec.Header().Get("Location"))
			}
			for _, name := range []string{sessionCookieName, pending2FACookieName} {
				cookie := cookieNamed(rec.Result().Cookies(), name)
				if cookie == nil || cookie.MaxAge >= 0 || cookie.Value != "" {
					t.Errorf("successful logout did not expire %s", name)
				}
			}
			if _, err := sessions.UserByToken(hashToken(current.Value), nowRFC3339()); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("logged-out session remains usable: %v", err)
			}
			if _, err := factors.PendingUser(hashToken(pending.Value), nowRFC3339()); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("logged-out pending challenge remains usable: %v", err)
			}
			rec = submit2FA(t, h, pending, "recovery="+recovery[0])
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" || cookieValue(rec, sessionCookieName) != "" {
				t.Error("challenge recreated a session after explicit logout without a password")
			}
			if count, err := factors.CountUnusedRecovery(1); err != nil || count != recoveryCodeCount {
				t.Fatalf("revoked challenge consumed a recovery code: %d %v", count, err)
			}
			if _, err := sessions.UserByToken(hashToken("other-browser"), nowRFC3339()); err != nil {
				t.Errorf("logout revoked another browser's session: %v", err)
			}
			if _, err := factors.PendingUser(hashToken("other-challenge"), nowRFC3339()); err != nil {
				t.Errorf("logout revoked another browser's pending challenge: %v", err)
			}
		})
	}
}
