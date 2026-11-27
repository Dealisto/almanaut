package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
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
	// CompareHashAndPassword silently ignores bytes beyond bcrypt's limit.
	// Enforce the same supported range on every verification path, including
	// the current-password check outside the login handler.
	if len(pw) > domain.MaxPasswordBytes {
		return false
	}
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
type sessionTokenCtxKey struct{}
type apiCredentialCtxKey struct{}

type apiCredential struct {
	tokens    *store.TokenRepo
	tokenHash string
}

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
			// A prior middleware (proxy/SSO auth) may already have resolved the
			// user; if so, don't require a session cookie.
			if _, ok := userFrom(r.Context()); ok {
				next.ServeHTTP(w, r)
				return
			}
			if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
				tokenHash := hashToken(c.Value)
				u, err := sessions.UserByToken(tokenHash, nowRFC3339())
				if err == nil {
					ctx := context.WithValue(withUser(r.Context(), u), sessionTokenCtxKey{}, tokenHash)
					next.ServeHTTP(w, r.WithContext(ctx))
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

// refreshUIAuth rechecks unsafe UI requests after CSRF has consumed their body.
// A slow form/upload must not retain a revoked session or a role removed while
// reading. Initial authentication still rejects anonymous requests before that
// work. This is not an atomic permission check through the later write/commit.
func refreshUIAuth(users *store.UserRepo, sessions *store.SessionRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if csrfSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			current, ok := userFrom(r.Context())
			var user domain.User
			err := store.ErrNotFound
			if ok {
				if tokenHash, session := r.Context().Value(sessionTokenCtxKey{}).(string); session {
					user, err = sessions.UserByToken(tokenHash, nowRFC3339())
				} else {
					// SSO already authenticated this local identity. Refresh its
					// role without re-provisioning a user deleted during the read
					// or falling back to a different browser-session identity.
					user, err = users.Get(current.ID)
				}
			}
			if errors.Is(err, store.ErrNotFound) {
				http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
				return
			}
			if err != nil {
				serverError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
		})
	}
}

type loginData struct {
	Title      string
	Next       string
	Error      string
	SSOEnabled bool
}

// loginForm renders the standalone login page.
func loginForm(ssoEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, r, "login.html", loginData{Title: "Sign in", Next: safeNext(r.URL.Query().Get("next")), SSOEnabled: ssoEnabled})
	}
}

// login verifies credentials, creates a session, and sets the cookie. Repeated
// failed attempts for a username are throttled (see loginThrottle): once a
// username is locked out the request is refused with 429 before any password
// verification, so an attacker cannot spend the server's bcrypt budget guessing.
func login(db *sql.DB, users *store.UserRepo, sessions *store.SessionRepo, totp *store.TOTPRepo, throttle *loginThrottle, audit *store.AuthEventRepo, forceSecure, ssoEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		release, ok := acquirePasswordWork(w, r)
		if !ok {
			return
		}
		defer release()
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		// Reject oversized input before bcrypt, throttle bookkeeping or auditing.
		if len(username) > domain.MaxUsernameBytes || len(password) > domain.MaxPasswordBytes {
			http.Error(w, "login fields are too long", http.StatusBadRequest)
			return
		}
		next := safeNext(r.FormValue("next"))
		now := time.Now().UTC()

		if ok, retryAfter := throttle.allowed(username, now); !ok {
			secs := int(math.Ceil(retryAfter.Seconds()))
			mins := (secs + 59) / 60
			recordAuth(audit, r, domain.AuthLoginFailure, username, 0, "rate-limited")
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			w.WriteHeader(http.StatusTooManyRequests)
			render(w, r, "login.html", loginData{
				Title: "Sign in", Next: next, SSOEnabled: ssoEnabled,
				Error: fmt.Sprintf("too many attempts — try again in %d minute(s)", mins),
			})
			return
		}

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
			throttle.recordFailure(username, now)
			// Prune stale buckets on the failure path too, so a spray across many
			// distinct usernames cannot grow the map unbounded when no successful
			// login ever occurs to trigger a prune.
			throttle.cleanup(now)
			recordAuth(audit, r, domain.AuthLoginFailure, username, 0, "")
			w.WriteHeader(http.StatusUnauthorized)
			render(w, r, "login.html", loginData{Title: "Sign in", Next: next, Error: "invalid username or password", SSOEnabled: ssoEnabled})
			return
		}

		token, err := newSessionToken()
		if err != nil {
			serverError(w, r, err)
			return
		}
		pending, err := issuePasswordLogin(db, users, sessions, totp, u, hashToken(token), now)
		if errors.Is(err, errLoginCredentialsChanged) {
			recordAuth(audit, r, domain.AuthLoginFailure, username, 0, "credentials changed")
			w.WriteHeader(http.StatusUnauthorized)
			render(w, r, "login.html", loginData{Title: "Sign in", Next: next, Error: "invalid username or password", SSOEnabled: ssoEnabled})
			return
		}
		if err != nil {
			serverError(w, r, err)
			return
		}
		nowStr := now.Format(time.RFC3339)
		if pending {
			// Keep throttle state until the second factor succeeds.
			_ = totp.DeleteExpiredPending(nowStr)
			setPending2FACookie(w, r, token, forceSecure)
			http.Redirect(w, r, "/login/2fa?next="+url.QueryEscape(next), http.StatusSeeOther)
			return
		}
		throttle.recordSuccess(username)
		_ = sessions.DeleteExpired(nowStr) // opportunistic prune
		throttle.cleanup(now)              // opportunistic prune of throttle state
		recordAuth(audit, r, domain.AuthLoginSuccess, u.Username, u.ID, "")
		setSessionCookie(w, r, token, forceSecure)
		http.Redirect(w, r, next, http.StatusSeeOther)
	}
}

var errLoginCredentialsChanged = errors.New("login credentials changed")

// issuePasswordLogin revalidates the verified credentials and reads the current
// second-factor policy under the same write lock as session/challenge creation.
// Bcrypt stays outside the transaction, so it does not block unrelated writers.
func issuePasswordLogin(db *sql.DB, users *store.UserRepo, sessions *store.SessionRepo, totp *store.TOTPRepo, verified domain.User, tokenHash string, now time.Time) (bool, error) {
	pending := false
	err := store.WithTx(db, func(tx *sql.Tx) error {
		current, err := users.WithTx(tx).Get(verified.ID)
		if errors.Is(err, store.ErrNotFound) {
			return errLoginCredentialsChanged
		}
		if err != nil {
			return err
		}
		if current.Username != verified.Username || current.PasswordHash != verified.PasswordHash {
			return errLoginCredentialsChanged
		}
		factor, err := totp.WithTx(tx).Get(current.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil && factor.Enabled {
			pending = true
			return totp.WithTx(tx).CreatePending(tokenHash, current.ID, now.Add(pending2FADuration).Format(time.RFC3339))
		}
		_, err = sessions.WithTx(tx).Create(store.Session{
			TokenHash: tokenHash, UserID: current.ID,
			CreatedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(sessionDuration).Format(time.RFC3339),
		})
		return err
	})
	return pending, err
}

// logout revokes the current browser's session and pending second-factor
// challenge together. An unfinished login in another tab must not restore a
// signed-out session using only a second factor after an explicit logout.
func logout(db *sql.DB, sessions *store.SessionRepo, totp *store.TOTPRepo, audit *store.AuthEventRepo, forceSecure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := store.WithTx(db, func(tx *sql.Tx) error {
			if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
				if err := sessions.WithTx(tx).DeleteByToken(hashToken(c.Value)); err != nil {
					return err
				}
			}
			if c, err := r.Cookie(pending2FACookieName); err == nil && c.Value != "" {
				return totp.WithTx(tx).DeletePending(hashToken(c.Value))
			}
			return nil
		})
		if err != nil {
			serverError(w, r, err)
			return
		}
		var username string
		var uid int64
		if u, ok := userFrom(r.Context()); ok {
			username, uid = u.Username, u.ID
		}
		recordAuth(audit, r, domain.AuthLogout, username, uid, "")
		clearSessionCookie(w, r, forceSecure)
		clearPending2FACookie(w, r, forceSecure)
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
	for i := 0; i < len(raw); i++ {
		// Browsers strip tabs and other URL controls before resolving a host.
		if raw[i] < 0x20 || raw[i] == 0x7f {
			return "/"
		}
	}
	return raw
}

// apiTokenPrefix marks almanaut API tokens so they are recognisable in logs and
// config. The random suffix carries the entropy; the prefix is not secret.
const apiTokenPrefix = "alm_"

// newAPIToken returns a new API token: the alm_ prefix followed by 32 bytes of
// crypto/rand entropy, base64url-encoded. The whole string is hashed for storage.
func newAPIToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return apiTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
// The scheme is matched case-insensitively per RFC 7235.
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):]), true
	}
	return "", false
}

// apiAuth authenticates /api requests. A bearer API token resolves to its owning
// user; failing that, a GET may fall back to the session cookie so a logged-in
// browser/dashboard keeps read access. Writes (any non-GET/HEAD method) require a
// bearer token — a write bearing only a session cookie is rejected. Because a
// browser never sends a bearer header automatically, this keeps /api free of CSRF
// without a CSRF token. All responses are JSON; a genuine backend failure is a
// 500, never a silent 401.
func apiAuth(tokens *store.TokenRepo, sessions *store.SessionRepo, audit *store.AuthEventRepo, tokenUses *tokenUseLog) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if raw, ok := bearerToken(r); ok {
				tokenHash := hashToken(raw)
				u, scope, err := tokens.UserByToken(tokenHash)
				if err == nil {
					if tokenUses != nil && tokenUses.shouldLog(tokenHash, time.Now().UTC()) {
						recordAuth(audit, r, domain.AuthTokenUsed, u.Username, u.ID, "scope="+scope)
					}
					ctx := withTokenScope(withUser(r.Context(), u), domain.Scope(scope))
					ctx = context.WithValue(ctx, apiCredentialCtxKey{}, apiCredential{tokens: tokens, tokenHash: tokenHash})
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				if !errors.Is(err, store.ErrNotFound) {
					apiServerError(w, r, err)
					return
				}
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			// No bearer token: writes are token-only.
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeJSONError(w, http.StatusUnauthorized, "api writes require a token")
				return
			}
			// Reads may use the session cookie.
			if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
				u, err := sessions.UserByToken(hashToken(c.Value), nowRFC3339())
				if err == nil {
					next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
					return
				}
				if !errors.Is(err, store.ErrNotFound) {
					apiServerError(w, r, err)
					return
				}
			}
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		})
	}
}

// refreshAPIBodyAuth checks the original bearer credential after body ingestion.
// Initial authentication/authorization still rejects disallowed requests before
// reading. No additional token-use audit is emitted. This protects the ingestion
// window, not permission changes after this check or during the later commit.
func refreshAPIBodyAuth(w http.ResponseWriter, r *http.Request, requiredScope domain.Scope) (*http.Request, bool) {
	credential, ok := r.Context().Value(apiCredentialCtxKey{}).(apiCredential)
	if !ok {
		return r, true // Authentication-disabled routes have no bearer identity.
	}
	user, scope, err := credential.tokens.UserByToken(credential.tokenHash)
	if errors.Is(err, store.ErrNotFound) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return r, false
	}
	if err != nil {
		apiServerError(w, r, err)
		return r, false
	}
	if !user.Role.CanWrite() || domain.Scope(scope) != requiredScope {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return r, false
	}
	ctx := withTokenScope(withUser(r.Context(), user), domain.Scope(scope))
	return r.WithContext(ctx), true
}
