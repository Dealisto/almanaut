package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/store"
)

func TestPasswordLoginRejectsRevokedCredentials(t *testing.T) {
	for _, secondFactor := range []bool{false, true} {
		for _, deleted := range []bool{false, true} {
			name := "password reset"
			if deleted {
				name = "deleted user"
			}
			if secondFactor {
				name += " with second factor"
			}
			t.Run(name, func(t *testing.T) {
				_, db := totpTestHandler(t)
				users, sessions, totp := store.NewUserRepo(db), store.NewSessionRepo(db), store.NewTOTPRepo(db)
				verified, err := users.Get(1)
				if err != nil {
					t.Fatal(err)
				}
				if secondFactor {
					if err := totp.SetSecret(1, "test-secret", nowRFC3339()); err != nil {
						t.Fatal(err)
					}
					if err := totp.Enable(1); err != nil {
						t.Fatal(err)
					}
				}
				if deleted {
					err = users.Delete(1)
				} else {
					err = setPasswordAndRevoke(db, users, sessions, httptest.NewRequest(http.MethodPost, "/", nil), 1, "replacement hash")
				}
				if err != nil {
					t.Fatal(err)
				}
				_, err = issuePasswordLogin(db, users, sessions, totp, verified, "stale-login", time.Now().UTC())
				if !errors.Is(err, errLoginCredentialsChanged) {
					t.Fatalf("stale credential result = %v", err)
				}
				for _, table := range []string{"sessions", "totp_pending"} {
					var count int
					if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
						t.Fatal(err)
					}
					if count != 0 {
						t.Errorf("revoked credentials created %d rows in %s", count, table)
					}
				}
			})
		}
	}
}

func TestPasswordLoginReadsCurrentSecondFactor(t *testing.T) {
	_, db := totpTestHandler(t)
	users, sessions, totp := store.NewUserRepo(db), store.NewSessionRepo(db), store.NewTOTPRepo(db)
	verified, err := users.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	// The factor is enabled after the password was verified.
	if err := totp.SetSecret(1, "test-secret", nowRFC3339()); err != nil {
		t.Fatal(err)
	}
	if err := totp.Enable(1); err != nil {
		t.Fatal(err)
	}
	pending, err := issuePasswordLogin(db, users, sessions, totp, verified, "pending-login", time.Now().UTC())
	if err != nil || !pending {
		t.Fatalf("new second factor bypassed: pending=%v err=%v", pending, err)
	}
	if _, err := sessions.UserByToken("pending-login", nowRFC3339()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session issued before second-factor verification: %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM totp_pending WHERE token_hash = ?", "pending-login").Scan(&count); err != nil || count != 1 {
		t.Fatalf("pending challenges = %d, err=%v", count, err)
	}
}
