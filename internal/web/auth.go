package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
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
