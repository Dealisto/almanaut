package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestTOTPFactorChangesRevokePreviousCredentials(t *testing.T) {
	for _, change := range []string{"enable", "disable", "reset"} {
		for _, failingTable := range []string{"", "sessions", "totp_pending"} {
			name := change
			if failingTable != "" {
				name += "/rollback_" + failingTable
			}
			t.Run(name, func(t *testing.T) {
				h, db := totpTestHandler(t)
				current := loginAs(t, h, "admin", "password123")
				factors := store.NewTOTPRepo(db)
				path, body, wantStatus := "/account/2fa/confirm", "", http.StatusOK
				if change == "enable" {
					if rec := postCSRF(t, h, current, "/account/2fa/setup", ""); rec.Code != http.StatusSeeOther {
						t.Fatalf("setup: %d", rec.Code)
					}
					factor, err := factors.Get(1)
					if err != nil {
						t.Fatal(err)
					}
					code, err := domain.TOTPCode(factor.Secret, time.Now().UTC())
					if err != nil {
						t.Fatal(err)
					}
					body = "code=" + code
				} else {
					_, codes := enroll2FA(t, h, db, current, 1)
					path, body, wantStatus = "/account/2fa/disable", "recovery="+codes[0], http.StatusSeeOther
					if change == "reset" {
						path, body = "/users/1/2fa/reset", ""
					}
				}
				// These represent credentials issued before the factor change.
				previous := &http.Cookie{Name: sessionCookieName, Value: "previous-session"}
				if _, err := store.NewSessionRepo(db).Create(store.Session{
					TokenHash: hashToken(previous.Value), UserID: 1, CreatedAt: nowRFC3339(),
					ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
				}); err != nil {
					t.Fatal(err)
				}
				if err := factors.CreatePending(hashToken("previous-challenge"), 1, time.Now().Add(time.Minute).UTC().Format(time.RFC3339)); err != nil {
					t.Fatal(err)
				}
				if failingTable != "" {
					if _, err := db.Exec(fmt.Sprintf("CREATE TRIGGER fail_revoke BEFORE DELETE ON %s BEGIN SELECT RAISE(ABORT, 'revocation unavailable'); END", failingTable)); err != nil {
						t.Fatal(err)
					}
					wantStatus = http.StatusInternalServerError
				}
				rec := postCSRF(t, h, current, path, body)
				if rec.Code != wantStatus {
					t.Fatalf("change: status %d, want %d; body %s", rec.Code, wantStatus, rec.Body)
				}
				if !sessionAlive(t, h, current) {
					t.Error("factor change revoked its own session")
				}
				_, pendingErr := factors.PendingUser(hashToken("previous-challenge"), nowRFC3339())
				if failingTable == "" {
					if sessionAlive(t, h, previous) {
						t.Error("previous session survived the factor change")
					}
					if !errors.Is(pendingErr, store.ErrNotFound) {
						t.Errorf("previous challenge survived the factor change: %v", pendingErr)
					}
					return
				}
				if !sessionAlive(t, h, previous) || pendingErr != nil {
					t.Errorf("failed change partially revoked credentials: pending error %v", pendingErr)
				}
				factor, err := factors.Get(1)
				if err != nil || factor.Enabled != (change != "enable") {
					t.Errorf("failed revocation changed factor: %+v, %v", factor, err)
				}
				wantCodes := recoveryCodeCount
				if change == "enable" {
					wantCodes = 0
				}
				if count, err := factors.CountUnusedRecovery(1); err != nil || count != wantCodes {
					t.Errorf("failed change altered recovery codes: count %d, err %v", count, err)
				}
				if strings.Contains(rec.Body.String(), "revocation unavailable") {
					t.Error("database error leaked to response")
				}
			})
		}
	}
}
