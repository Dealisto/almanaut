package web

import (
	"crypto/rand"
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
// admin's password (a lockout recovery valve driven by ALMANAUT_RESET_ADMIN).
func BootstrapAdmin(users *store.UserRepo, logger *log.Logger, envUser, envPass string, reset bool) error {
	n, err := users.Count()
	if err != nil {
		return err
	}
	username := envUser
	if username == "" {
		username = "admin"
	}

	if n == 0 {
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

	// Reset path: prefer the named user, else the oldest account.
	target, err := users.GetByUsername(username)
	if errors.Is(err, store.ErrNotFound) {
		list, lerr := users.List()
		if lerr != nil {
			return lerr
		}
		if len(list) == 0 {
			return nil
		}
		target = list[0]
	} else if err != nil {
		return err
	}
	password, _, err := passwordOrGenerated(envPass)
	if err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := users.UpdatePassword(target.ID, hash, nowRFC3339()); err != nil {
		return err
	}
	logger.Printf("========================================================")
	logger.Printf("Almanaut reset the password for admin account %q.", target.Username)
	logger.Printf("  password: %s", password)
	logger.Printf("========================================================")
	return nil
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
