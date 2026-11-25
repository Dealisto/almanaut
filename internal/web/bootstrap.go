package web

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"log"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// BootstrapAdmin ensures an initial admin account exists. On an empty user
// table it creates one (from envUser/envPass when set, otherwise username
// "admin" with a generated password printed to the log). When users already
// exist it does nothing, unless reset is true, in which case it resets the
// admin's password and makes sure that account is an admin (a lockout recovery
// valve driven by ALMANAUT_RESET_ADMIN, which must also recover from the last
// admin having been demoted). Resetting also revokes all sessions and pending
// second-factor challenges, while keeping enrolled 2FA and API tokens.
func BootstrapAdmin(db *sql.DB, logger *log.Logger, envUser, envPass string, reset bool) error {
	users := store.NewUserRepo(db)
	n, err := users.Count()
	if err != nil {
		return err
	}
	username := envUser
	if username == "" {
		username = "admin"
	}

	if n == 0 {
		if err := (domain.User{Username: username, Role: domain.RoleAdmin}).Validate(); err != nil {
			return err
		}
		password, generated, err := passwordOrGenerated(envPass)
		if err != nil {
			return err
		}
		hash, err := hashPassword(password)
		if err != nil {
			return err
		}
		now := nowRFC3339()
		if _, err := users.Create(domain.User{
			Username: username, Role: domain.RoleAdmin, PasswordHash: hash, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		if generated {
			logger.Printf("========================================================")
			logger.Printf("Almanaut created an initial admin account.")
			logger.Printf("  username: %s", username)
			logger.Printf("  password: %s", password)
			logger.Printf("Log in and change it. This is shown only once.")
			logger.Printf("========================================================")
		} else {
			logger.Printf("Almanaut seeded the initial admin %q from ALMANAUT_AUTH_USER/PASS.", username)
		}
		return nil
	}

	if !reset {
		return nil
	}

	password, _, err := passwordOrGenerated(envPass)
	if err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	var target domain.User
	// Resolve the target and replace credentials in the same transaction as
	// revoking sessions and password-verified second-factor challenges. A
	// failed reset must not leave a partially updated account behind.
	err = store.WithTx(db, func(tx *sql.Tx) error {
		ur := users.WithTx(tx)
		var err error
		target, err = ur.GetByUsername(username)
		if errors.Is(err, store.ErrNotFound) {
			list, err := ur.List()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				return nil
			}
			// Prefer the oldest admin, falling back to the oldest account.
			target = oldestUser(list)
		} else if err != nil {
			return err
		}
		if err := ur.UpdatePassword(target.ID, hash, nowRFC3339()); err != nil {
			return err
		}
		if target.Role != domain.RoleAdmin {
			if err := ur.UpdateRole(target.ID, domain.RoleAdmin, nowRFC3339()); err != nil {
				return err
			}
		}
		if err := store.NewSessionRepo(db).WithTx(tx).DeleteByUser(target.ID); err != nil {
			return err
		}
		return store.NewTOTPRepo(db).WithTx(tx).DeletePendingByUser(target.ID)
	})
	if err != nil {
		return err
	}
	if target.ID == 0 {
		return nil
	}
	if target.Role != domain.RoleAdmin {
		logger.Printf("Almanaut promoted %q to admin (it was %s).", target.Username, target.Role)
	}
	logger.Printf("========================================================")
	logger.Printf("Almanaut reset the password for admin account %q.", target.Username)
	logger.Printf("  password: %s", password)
	logger.Printf("========================================================")
	return nil
}

// oldestUser returns the earliest-created admin in list, or the earliest-created
// user when no admin exists. IDs are AUTOINCREMENT, so the lowest is oldest.
func oldestUser(list []domain.User) domain.User {
	var oldest, oldestAdmin *domain.User
	for i := range list {
		u := &list[i]
		if oldest == nil || u.ID < oldest.ID {
			oldest = u
		}
		if u.Role == domain.RoleAdmin && (oldestAdmin == nil || u.ID < oldestAdmin.ID) {
			oldestAdmin = u
		}
	}
	if oldestAdmin != nil {
		return *oldestAdmin
	}
	return *oldest
}

// passwordOrGenerated returns envPass unchanged when set, otherwise a fresh
// random password with generated=true.
func passwordOrGenerated(envPass string) (pw string, generated bool, err error) {
	if envPass != "" {
		return envPass, false, nil
	}
	pw, err = randomPassword()
	return pw, true, err
}

// randomPassword returns a 24-character base64url password (18 random bytes).
func randomPassword() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
