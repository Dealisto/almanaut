package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// hashPassword returns a bcrypt hash of pw at the default cost.
func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// verifyPassword reports whether pw matches the bcrypt hash.
func verifyPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummyPasswordHash is compared against when a login names an unknown user, so
// the request pays the same bcrypt cost as a real user with a wrong password —
// closing a username-enumeration timing oracle.
var dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("almanaut-timing-equalizer"), bcrypt.DefaultCost)

// hashToken returns the sha256 (hex) of a session token. Sessions are stored by
// this hash so the database never holds a usable token.
func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// newSessionToken returns 32 bytes of crypto/rand entropy, base64url-encoded.
func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// sessionCookieName is the cookie carrying the opaque session token.
const sessionCookieName = "almanaut_session"

// sessionDuration is how long a new session stays valid.
const sessionDuration = 30 * 24 * time.Hour

type userCtxKey struct{}

// withUser returns a context carrying the authenticated user.
func withUser(ctx context.Context, u domain.User) context.Context {
	return context.WithValue(ctx, userCtxKey{}, u)
}

// userFrom returns the authenticated user placed in the context by sessionAuth,
// and false when the request is unauthenticated.
func userFrom(ctx context.Context) (domain.User, bool) {
	u, ok := ctx.Value(userCtxKey{}).(domain.User)
	return u, ok
}

// setSessionCookie writes the session cookie. Secure follows the same logic as
// the CSRF cookie: on behind a TLS-terminating proxy (forceSecure) or a direct
// TLS connection.
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, forceSecure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   forceSecure || r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
}

// clearSessionCookie expires the session cookie (logout).
func clearSessionCookie(w http.ResponseWriter, r *http.Request, forceSecure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   forceSecure || r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// sessionAuth resolves the session cookie to a user and puts it in the request
// context. Unauthenticated requests get a 303 redirect to /login (pages) or a
// 401 JSON error (/api/*). A genuine backend failure is a 500, never a silent
// redirect.
func sessionAuth(sessions *store.SessionRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
				u, err := sessions.UserByToken(hashToken(c.Value), nowRFC3339())
				if err == nil {
					next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
					return
				}
				if !errors.Is(err, store.ErrNotFound) {
					if strings.HasPrefix(r.URL.Path, "/api/") {
						apiServerError(w, r, err)
					} else {
						serverError(w, r, err)
					}
					return
				}
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		})
	}
}

type loginData struct {
	Title string
	Next  string
	Error string
}

// loginForm renders the standalone login page.
func loginForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, "login.html", loginData{Title: "Sign in", Next: safeNext(r.URL.Query().Get("next"))})
}

// login verifies credentials, creates a session, and sets the cookie.
func login(users *store.UserRepo, sessions *store.SessionRepo, forceSecure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		next := safeNext(r.FormValue("next"))

		u, err := users.GetByUsername(username)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			serverError(w, r, err)
			return
		}
		hash := string(dummyPasswordHash)
		if err == nil {
			hash = u.PasswordHash
		}
		if !verifyPassword(hash, password) {
			w.WriteHeader(http.StatusUnauthorized)
			render(w, r, "login.html", loginData{Title: "Sign in", Next: next, Error: "invalid username or password"})
			return
		}

		token, err := newSessionToken()
		if err != nil {
			serverError(w, r, err)
			return
		}
		now := nowRFC3339()
		expires := time.Now().UTC().Add(sessionDuration).Format(time.RFC3339)
		if _, err := sessions.Create(store.Session{
			TokenHash: hashToken(token), UserID: u.ID, CreatedAt: now, ExpiresAt: expires,
		}); err != nil {
			serverError(w, r, err)
			return
		}
		_ = sessions.DeleteExpired(now) // opportunistic prune
		setSessionCookie(w, r, token, forceSecure)
		http.Redirect(w, r, next, http.StatusSeeOther)
	}
}

// logout deletes the current session and clears the cookie.
func logout(sessions *store.SessionRepo, forceSecure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
			_ = sessions.DeleteByToken(hashToken(c.Value))
		}
		clearSessionCookie(w, r, forceSecure)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

// safeNext returns raw only when it is a safe local path (single leading slash),
// guarding against open-redirect via "//host", absolute URLs, or a backslash /
// control character that a browser may normalize into a protocol-relative URL.
func safeNext(raw string) string {
	if raw == "" || raw[0] != '/' || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n") {
		return "/"
	}
	return raw
}
