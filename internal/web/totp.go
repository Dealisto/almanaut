package web

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"rsc.io/qr"
)

const (
	pending2FACookieName = "almanaut_2fa"
	pending2FADuration   = 5 * time.Minute
	totpIssuer           = "Almanaut"
	recoveryCodeCount    = 10
)

// setPending2FACookie writes the short-lived post-password / pre-2FA cookie.
func setPending2FACookie(w http.ResponseWriter, r *http.Request, token string, forceSecure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: pending2FACookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: forceSecure || r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: int(pending2FADuration.Seconds()),
	})
}

func clearPending2FACookie(w http.ResponseWriter, r *http.Request, forceSecure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: pending2FACookieName, Value: "", Path: "/",
		HttpOnly: true, Secure: forceSecure || r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// totpQRDataURI renders an otpauth URI as a base64 PNG data URI for an <img>.
func totpQRDataURI(uri string) (string, error) {
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}

type account2FAData struct {
	Title        string
	Error        string
	Enabled      bool
	Enrolling    bool
	Secret       string
	URI          string
	QR           string
	RecoveryLeft int
}

// account2FA shows the current 2FA status and the enrollment UI.
func account2FA(totp *store.TOTPRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r.Context())
		t, err := totp.Get(u.ID)
		if errors.Is(err, store.ErrNotFound) {
			render(w, r, "account_2fa.html", account2FAData{Title: "Two-factor authentication"})
			return
		}
		if err != nil {
			serverError(w, r, err)
			return
		}
		if t.Enabled {
			left, err := totp.CountUnusedRecovery(u.ID)
			if err != nil {
				serverError(w, r, err)
				return
			}
			render(w, r, "account_2fa.html", account2FAData{Title: "Two-factor authentication", Enabled: true, RecoveryLeft: left})
			return
		}
		// Enrollment pending: show the QR for the stored (unconfirmed) secret.
		uri := domain.TOTPURI(totpIssuer, u.Username, t.Secret)
		qrImg, err := totpQRDataURI(uri)
		if err != nil {
			serverError(w, r, err)
			return
		}
		render(w, r, "account_2fa.html", account2FAData{
			Title: "Two-factor authentication", Enrolling: true,
			Secret: t.Secret, URI: uri, QR: qrImg,
		})
	}
}

// setup2FA begins enrollment: generate a secret and store it unconfirmed.
func setup2FA(totp *store.TOTPRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r.Context())
		// Active factors must go through disable, which re-verifies a current
		// code, before re-enrollment. Avoid generating a needless secret here;
		// SetSecret also enforces this guard atomically if confirmation races us.
		if t, err := totp.Get(u.ID); err == nil && t.Enabled {
			http.Redirect(w, r, "/account/2fa", http.StatusSeeOther)
			return
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			serverError(w, r, err)
			return
		}
		secret, err := domain.GenerateTOTPSecret()
		if err != nil {
			serverError(w, r, err)
			return
		}
		if err := totp.SetSecret(u.ID, secret, nowRFC3339()); err != nil {
			serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/account/2fa", http.StatusSeeOther)
	}
}

type recoveryCodesData struct {
	Title string
	Codes []string
}

var (
	errTOTPAlreadyEnabled = errors.New("two-factor authentication is already enabled")
	errTOTPInvalidCode    = errors.New("invalid enrollment code")
)

// confirm2FA verifies the first code, enables 2FA, and shows recovery codes once.
func confirm2FA(totp *store.TOTPRepo, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r.Context())
		var pending domain.UserTOTP
		var codes []string
		code := r.FormValue("code")
		// Read the enrollment and enable it in one transaction. A repeated or
		// concurrent confirmation must neither disclose an active secret nor
		// replace the recovery codes returned to the first successful caller.
		err := store.WithTx(db, func(tx *sql.Tx) error {
			tr := totp.WithTx(tx)
			var err error
			pending, err = tr.Get(u.ID)
			if err != nil {
				return err
			}
			if pending.Enabled {
				return errTOTPAlreadyEnabled
			}
			step, ok := domain.MatchTOTP(pending.Secret, code, time.Now().UTC())
			if !ok {
				return errTOTPInvalidCode
			}
			codes, err = domain.GenerateRecoveryCodes(recoveryCodeCount)
			if err != nil {
				return err
			}
			hashes := make([]string, len(codes))
			for i, c := range codes {
				hashes[i] = hashToken(domain.NormalizeRecoveryCode(c))
			}
			if err := tr.Enable(u.ID); err != nil {
				return err
			}
			fresh, err := tr.UseStep(u.ID, step)
			if err != nil {
				return err
			}
			if !fresh {
				return errTOTPInvalidCode
			}
			if err := tr.ReplaceRecoveryCodes(u.ID, hashes); err != nil {
				return err
			}
			return revokeFactorCredentials(tr, store.NewSessionRepo(db).WithTx(tx), r, u.ID)
		})
		if errors.Is(err, errTOTPAlreadyEnabled) {
			http.Redirect(w, r, "/account/2fa", http.StatusSeeOther)
			return
		}
		if errors.Is(err, errTOTPInvalidCode) {
			uri := domain.TOTPURI(totpIssuer, u.Username, pending.Secret)
			qrImg, qerr := totpQRDataURI(uri)
			if qerr != nil {
				serverError(w, r, qerr)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			render(w, r, "account_2fa.html", account2FAData{
				Title: "Two-factor authentication", Enrolling: true,
				Error:  "Invalid code. Enter the current 6-digit code from your authenticator and try again.",
				Secret: pending.Secret, URI: uri, QR: qrImg,
			})
			return
		}
		if err != nil {
			notFoundOrServerError(w, r, "enrollment", err)
			return
		}
		render(w, r, "2fa_recovery.html", recoveryCodesData{Title: "Recovery codes", Codes: codes})
	}
}

// disable2FA turns 2FA off after re-verifying a current code or recovery code,
// so a hijacked session cannot silently remove the second factor.
func disable2FA(totp *store.TOTPRepo, db *sql.DB) http.HandlerFunc {
	// Factor changes have their own per-user budget: a successful password
	// login must not clear failed attempts to remove the second factor.
	throttle := newLoginThrottle()
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r.Context())
		key := strconv.FormatInt(u.ID, 10)
		code, recovery := r.FormValue("code"), r.FormValue("recovery")
		var t domain.UserTOTP
		// Verification consumes one-time credentials. Keep consumption and the
		// delete together so a storage error leaves the credential usable.
		err := store.WithTx(db, func(tx *sql.Tx) error {
			now := time.Now().UTC()
			if ok, _ := throttle.allowed(key, now); !ok {
				return errTOTPDisableThrottled
			}
			tr := totp.WithTx(tx)
			var err error
			t, err = tr.Get(u.ID)
			if err != nil {
				return err
			}
			valid, err := verifySecondFactor(tr, u.ID, t.Secret, code, recovery)
			if err != nil {
				return err
			}
			if !valid {
				// Record before releasing the write transaction, so concurrent
				// attempts observe the lockout before verifying another code.
				throttle.recordFailure(key, now)
				throttle.cleanup(now)
				return errTOTPDisableInvalid
			}
			if err := tr.Disable(u.ID); err != nil {
				return err
			}
			return revokeFactorCredentials(tr, store.NewSessionRepo(db).WithTx(tx), r, u.ID)
		})
		if errors.Is(err, errTOTPDisableThrottled) {
			http.Error(w, "Too many attempts. Try again later.", http.StatusTooManyRequests)
			return
		}
		if errors.Is(err, errTOTPDisableInvalid) {
			left, err := totp.CountUnusedRecovery(u.ID)
			if err != nil {
				serverError(w, r, err)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			render(w, r, "account_2fa.html", account2FAData{
				Title: "Two-factor authentication", Enabled: t.Enabled, RecoveryLeft: left,
				Error: "Invalid code. Try a current authenticator code or an unused recovery code.",
			})
			return
		}
		if err != nil {
			notFoundOrServerError(w, r, "2fa", err)
			return
		}
		throttle.recordSuccess(key)
		http.Redirect(w, r, "/account/2fa", http.StatusSeeOther)
	}
}

var (
	errTOTPDisableThrottled = errors.New("second-factor removal is rate-limited")
	errTOTPDisableInvalid   = errors.New("invalid second factor for removal")
)

// revokeFactorCredentials invalidates credentials issued under the previous
// second-factor policy. Keep only the session performing its own factor change.
// In particular, a pending challenge must never become usable again after a
// reset followed by re-enrollment. Both repositories must use the transaction
// that changes the factor, so a failed revocation rolls the whole change back.
func revokeFactorCredentials(totp *store.TOTPRepo, sessions *store.SessionRepo, r *http.Request, userID int64) error {
	if err := totp.DeletePendingByUser(userID); err != nil {
		return err
	}
	return revokeSessions(sessions, r, userID)
}

// verifySecondFactor checks a TOTP code or, failing that, redeems a single-use
// recovery code. Returns true on either. A TOTP code is accepted once per time
// step, so a code observed in use cannot be replayed while it is still valid.
func verifySecondFactor(totp *store.TOTPRepo, userID int64, secret, code, recovery string) (bool, error) {
	if code != "" {
		if step, ok := domain.MatchTOTP(secret, code, time.Now().UTC()); ok {
			fresh, err := totp.UseStep(userID, step)
			if err != nil || fresh {
				return fresh, err
			}
		}
	}
	if recovery != "" {
		return totp.UseRecoveryCode(userID, hashToken(domain.NormalizeRecoveryCode(recovery)))
	}
	return false, nil
}

type challenge2FAData struct {
	Title string
	Next  string
	Error string
}

// challenge2FAForm renders the login second-factor challenge. It requires a
// valid pending cookie (set by login after a correct password).
func challenge2FAForm(totp *store.TOTPRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		valid, err := hasValidPending2FA(totp, r)
		if err != nil {
			serverError(w, r, err)
			return
		}
		if !valid {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		render(w, r, "2fa_challenge.html", challenge2FAData{Title: "Two-factor authentication", Next: safeNext(r.URL.Query().Get("next"))})
	}
}

func hasValidPending2FA(totp *store.TOTPRepo, r *http.Request) (bool, error) {
	c, err := r.Cookie(pending2FACookieName)
	if err != nil || c.Value == "" {
		return false, nil
	}
	_, err = totp.PendingUser(hashToken(c.Value), nowRFC3339())
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

var (
	errTOTPChallengeInvalid = errors.New("invalid pending second-factor challenge")
	errTOTPLoginThrottled   = errors.New("second-factor login is rate-limited")
	errTOTPLoginInvalid     = errors.New("invalid second factor")
)

// verify2FA completes login atomically: it consumes the factor and pending
// challenge only when the new session is successfully persisted.
func verify2FA(users *store.UserRepo, sessions *store.SessionRepo, totp *store.TOTPRepo, db *sql.DB, throttle *loginThrottle, audit *store.AuthEventRepo, forceSecure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next := safeNext(r.FormValue("next"))
		code, recovery := r.FormValue("code"), r.FormValue("recovery")
		c, err := r.Cookie(pending2FACookieName)
		if err != nil || c.Value == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		token, err := newSessionToken()
		if err != nil {
			serverError(w, r, err)
			return
		}
		now := time.Now().UTC()
		var u domain.User
		err = store.WithTx(db, func(tx *sql.Tx) error {
			tr := totp.WithTx(tx)
			uid, err := tr.PendingUser(hashToken(c.Value), now.Format(time.RFC3339))
			if errors.Is(err, store.ErrNotFound) {
				return errTOTPChallengeInvalid
			}
			if err != nil {
				return err
			}
			u, err = users.WithTx(tx).Get(uid)
			if err != nil {
				return err
			}
			if ok, _ := throttle.allowed(u.Username, now); !ok {
				return errTOTPLoginThrottled
			}
			t, err := tr.Get(uid)
			if errors.Is(err, store.ErrNotFound) || (err == nil && !t.Enabled) {
				return errTOTPChallengeInvalid
			}
			if err != nil {
				return err
			}
			valid, err := verifySecondFactor(tr, uid, t.Secret, code, recovery)
			if err != nil {
				return err
			}
			if !valid {
				// Publish failed attempts before releasing the write lock so
				// concurrent challenges cannot verify past the failure budget.
				throttle.recordFailure(u.Username, now)
				throttle.cleanup(now)
				return errTOTPLoginInvalid
			}
			if err := tr.DeletePending(hashToken(c.Value)); err != nil {
				return err
			}
			_, err = sessions.WithTx(tx).Create(store.Session{
				TokenHash: hashToken(token), UserID: uid, CreatedAt: now.Format(time.RFC3339),
				ExpiresAt: now.Add(sessionDuration).Format(time.RFC3339),
			})
			return err
		})
		switch {
		case errors.Is(err, errTOTPChallengeInvalid):
			clearPending2FACookie(w, r, forceSecure)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		case errors.Is(err, errTOTPLoginThrottled):
			w.WriteHeader(http.StatusTooManyRequests)
			render(w, r, "2fa_challenge.html", challenge2FAData{Title: "Two-factor authentication", Next: next, Error: "too many attempts — try again later"})
			return
		case errors.Is(err, errTOTPLoginInvalid):
			recordAuth(audit, r, domain.Auth2FAFailure, u.Username, u.ID, "")
			w.WriteHeader(http.StatusUnauthorized)
			render(w, r, "2fa_challenge.html", challenge2FAData{Title: "Two-factor authentication", Next: next, Error: "invalid code"})
			return
		case err != nil:
			serverError(w, r, err)
			return
		}
		throttle.recordSuccess(u.Username)
		clearPending2FACookie(w, r, forceSecure)
		recordAuth(audit, r, domain.Auth2FASuccess, u.Username, u.ID, "")
		recordAuth(audit, r, domain.AuthLoginSuccess, u.Username, u.ID, "2fa")
		setSessionCookie(w, r, token, forceSecure)
		http.Redirect(w, r, next, http.StatusSeeOther)
	}
}

// resetUser2FA lets an admin remove a user's second factor (e.g. lost device).
// It also ends that user's sessions, in the same transaction: a reset must not
// leave a session that was opened with the old factor alive.
func resetUser2FA(totp *store.TOTPRepo, sessions *store.SessionRepo, audit *store.AuthEventRepo, users *store.UserRepo, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := userIDParam(w, r)
		if !ok {
			return
		}
		var target domain.User
		err := store.WithTx(db, func(tx *sql.Tx) error {
			var err error
			if target, err = users.WithTx(tx).Get(id); err != nil {
				return err
			}
			if err := totp.WithTx(tx).Disable(id); err != nil {
				return err
			}
			return revokeFactorCredentials(totp.WithTx(tx), sessions.WithTx(tx), r, id)
		})
		if err != nil {
			notFoundOrServerError(w, r, "user", err)
			return
		}
		recordAuth(audit, r, domain.AuthSessionRevoked, target.Username, id, "2fa reset by "+actor(r))
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}
