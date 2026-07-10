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
	"github.com/go-chi/chi/v5"
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
		// Refuse to re-initialize an already-enabled factor: SetSecret would reset
		// enabled to 0, which would be an unguarded way to neutralize 2FA from a
		// hijacked session. Re-enrollment must go through disable (which re-verifies
		// a current code) first.
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

// confirm2FA verifies the first code, enables 2FA, and shows recovery codes once.
func confirm2FA(totp *store.TOTPRepo, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r.Context())
		t, err := totp.Get(u.ID)
		if err != nil {
			notFoundOrServerError(w, r, "enrollment", err)
			return
		}
		if !domain.VerifyTOTP(t.Secret, r.FormValue("code"), time.Now().UTC()) {
			// Re-show the enrollment page with an error.
			uri := domain.TOTPURI(totpIssuer, u.Username, t.Secret)
			qrImg, qerr := totpQRDataURI(uri)
			if qerr != nil {
				serverError(w, r, qerr)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			render(w, r, "account_2fa.html", account2FAData{
				Title: "Two-factor authentication", Enrolling: true,
				Secret: t.Secret, URI: uri, QR: qrImg,
			})
			return
		}
		codes, err := domain.GenerateRecoveryCodes(recoveryCodeCount)
		if err != nil {
			serverError(w, r, err)
			return
		}
		hashes := make([]string, len(codes))
		for i, c := range codes {
			hashes[i] = hashToken(domain.NormalizeRecoveryCode(c))
		}
		// Enable + store recovery codes atomically.
		err = store.WithTx(db, func(tx *sql.Tx) error {
			tr := totp.WithTx(tx)
			if err := tr.Enable(u.ID); err != nil {
				return err
			}
			return tr.ReplaceRecoveryCodes(u.ID, hashes)
		})
		if err != nil {
			serverError(w, r, err)
			return
		}
		render(w, r, "2fa_recovery.html", recoveryCodesData{Title: "Recovery codes", Codes: codes})
	}
}

// disable2FA turns 2FA off after re-verifying a current code or recovery code,
// so a hijacked session cannot silently remove the second factor.
func disable2FA(totp *store.TOTPRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r.Context())
		t, err := totp.Get(u.ID)
		if err != nil {
			notFoundOrServerError(w, r, "2fa", err)
			return
		}
		if !verifySecondFactor(totp, u.ID, t.Secret, r.FormValue("code"), r.FormValue("recovery")) {
			http.Error(w, "invalid code", http.StatusBadRequest)
			return
		}
		if err := totp.Disable(u.ID); err != nil {
			serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/account/2fa", http.StatusSeeOther)
	}
}

// verifySecondFactor checks a TOTP code or, failing that, redeems a single-use
// recovery code. Returns true on either.
func verifySecondFactor(totp *store.TOTPRepo, userID int64, secret, code, recovery string) bool {
	if code != "" && domain.VerifyTOTP(secret, code, time.Now().UTC()) {
		return true
	}
	if recovery != "" {
		ok, err := totp.UseRecoveryCode(userID, hashToken(domain.NormalizeRecoveryCode(recovery)))
		return err == nil && ok
	}
	return false
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
		if !hasValidPending2FA(totp, r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		render(w, r, "2fa_challenge.html", challenge2FAData{Title: "Two-factor authentication", Next: safeNext(r.URL.Query().Get("next"))})
	}
}

func hasValidPending2FA(totp *store.TOTPRepo, r *http.Request) bool {
	c, err := r.Cookie(pending2FACookieName)
	if err != nil || c.Value == "" {
		return false
	}
	_, err = totp.PendingUser(hashToken(c.Value), nowRFC3339())
	return err == nil
}

// verify2FA completes login: it validates the second factor for the pending
// user, then creates the real session.
func verify2FA(users *store.UserRepo, sessions *store.SessionRepo, totp *store.TOTPRepo, throttle *loginThrottle, audit *store.AuthEventRepo, forceSecure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next := safeNext(r.FormValue("next"))
		c, err := r.Cookie(pending2FACookieName)
		if err != nil || c.Value == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		now := time.Now().UTC()
		uid, err := totp.PendingUser(hashToken(c.Value), now.Format(time.RFC3339))
		if errors.Is(err, store.ErrNotFound) {
			clearPending2FACookie(w, r, forceSecure)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if err != nil {
			serverError(w, r, err)
			return
		}
		u, err := users.Get(uid)
		if err != nil {
			serverError(w, r, err)
			return
		}
		if ok, _ := throttle.allowed(u.Username, now); !ok {
			w.WriteHeader(http.StatusTooManyRequests)
			render(w, r, "2fa_challenge.html", challenge2FAData{Title: "Two-factor authentication", Next: next, Error: "too many attempts — try again later"})
			return
		}
		t, err := totp.Get(uid)
		if err != nil {
			serverError(w, r, err)
			return
		}
		if !verifySecondFactor(totp, uid, t.Secret, r.FormValue("code"), r.FormValue("recovery")) {
			throttle.recordFailure(u.Username, now)
			throttle.cleanup(now)
			recordAuth(audit, r, domain.Auth2FAFailure, u.Username, uid, "")
			w.WriteHeader(http.StatusUnauthorized)
			render(w, r, "2fa_challenge.html", challenge2FAData{Title: "Two-factor authentication", Next: next, Error: "invalid code"})
			return
		}
		// Success: consume the pending challenge and start the real session.
		throttle.recordSuccess(u.Username)
		_ = totp.DeletePending(hashToken(c.Value))
		clearPending2FACookie(w, r, forceSecure)
		token, err := newSessionToken()
		if err != nil {
			serverError(w, r, err)
			return
		}
		nowStr := now.Format(time.RFC3339)
		if _, err := sessions.Create(store.Session{
			TokenHash: hashToken(token), UserID: uid, CreatedAt: nowStr,
			ExpiresAt: now.Add(sessionDuration).Format(time.RFC3339),
		}); err != nil {
			serverError(w, r, err)
			return
		}
		recordAuth(audit, r, domain.Auth2FASuccess, u.Username, uid, "")
		recordAuth(audit, r, domain.AuthLoginSuccess, u.Username, uid, "2fa")
		setSessionCookie(w, r, token, forceSecure)
		http.Redirect(w, r, next, http.StatusSeeOther)
	}
}

// resetUser2FA lets an admin remove a user's second factor (e.g. lost device).
func resetUser2FA(totp *store.TOTPRepo, audit *store.AuthEventRepo, users *store.UserRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := totp.Disable(id); err != nil {
			serverError(w, r, err)
			return
		}
		target, _ := users.Get(id)
		recordAuth(audit, r, domain.AuthSessionRevoked, target.Username, id, "2fa reset by "+actor(r))
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}
