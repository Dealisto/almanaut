package store

import (
	"errors"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestAuthenticationExpiryUsesChronologicalOrder(t *testing.T) {
	for _, kind := range []string{"session", "pending TOTP"} {
		t.Run(kind, func(t *testing.T) {
			db := newTestDB(t)
			uid, err := NewUserRepo(db).Create(domain.User{Username: "user", Role: domain.RoleViewer})
			if err != nil {
				t.Fatal(err)
			}
			sessions, totp := NewSessionRepo(db), NewTOTPRepo(db)
			for _, tc := range []struct {
				name, expiry, now string
				active            bool
			}{
				{"expired positive offset", "2026-07-10T14:00:00+02:00", "2026-07-10T13:00:00Z", false},
				{"active negative offset", "2026-07-10T10:00:00-04:00", "2026-07-10T13:00:00Z", true},
				{"equal with offset", "2026-07-10T14:00:00+02:00", "2026-07-10T12:00:00Z", false},
				{"offset on current time", "2026-07-10T13:00:00Z", "2026-07-10T14:00:00+02:00", true},
				{"fractional future", "2026-07-10T12:00:00.500Z", "2026-07-10T12:00:00Z", true},
				{"fractional past", "2026-07-10T12:00:00Z", "2026-07-10T12:00:00.500Z", false},
				{"malformed stored expiry", "zzz-invalid", "2026-07-10T12:00:00Z", false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					// Insert a legacy row directly: auth must correctly read older
					// values as well as values normalized by current writers.
					table := "sessions"
					query := `INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, '')`
					lookup := func() error { _, err := sessions.UserByToken(tc.name, tc.now); return err }
					prune := sessions.DeleteExpired
					if kind == "pending TOTP" {
						table = "totp_pending"
						query = `INSERT INTO totp_pending (token_hash, user_id, expires_at) VALUES (?, ?, ?)`
						lookup = func() error { _, err := totp.PendingUser(tc.name, tc.now); return err }
						prune = totp.DeleteExpiredPending
					}
					if _, err := db.Exec(query, tc.name, uid, tc.expiry); err != nil {
						t.Fatal(err)
					}
					if err := lookup(); tc.active && err != nil || !tc.active && !errors.Is(err, ErrNotFound) {
						t.Errorf("active=%v, lookup error=%v", tc.active, err)
					}
					if err := prune(tc.now); err != nil {
						t.Fatal(err)
					}
					want := 0
					if tc.active {
						want = 1
					}
					if got := countRows(t, db, `SELECT COUNT(*) FROM `+table+` WHERE token_hash = ?`, tc.name); got != want {
						t.Errorf("pruning left %d records, want %d", got, want)
					}
				})
			}
		})
	}
}
