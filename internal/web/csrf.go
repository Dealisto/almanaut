package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"time"
)

const (
	csrfCookieName = "csrf_token"
	csrfFieldName  = "csrf_token"
)

type csrfCtxKey struct{}

// csrfTokenFrom returns the per-request CSRF token placed in the context by
// csrfProtect, or "" if none.
func csrfTokenFrom(ctx context.Context) string {
	tok, _ := ctx.Value(csrfCtxKey{}).(string)
	return tok
}

// generateCSRFToken returns 32 bytes of crypto/rand entropy, base64url-encoded.
func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// csrfProtect is stateless double-submit-cookie CSRF protection. Every request
// is guaranteed a csrf_token cookie and a context token. Unsafe methods must
// carry a csrf_token form field equal to the cookie, else they get 403.
//
// forceSecure sets the Secure flag on the cookie unconditionally; it is meant
// for deployments behind a TLS-terminating reverse proxy, where the connection
// this process sees is plaintext even though the client speaks HTTPS. A direct
// TLS connection (r.TLS != nil) sets Secure on its own regardless of the flag.
func csrfProtect(forceSecure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ""
			if c, err := r.Cookie(csrfCookieName); err == nil {
				token = c.Value
			}
			// Mint and set the cookie only on safe methods. An unsafe request with no
			// cookie cannot carry a matching token, so it is rejected below regardless
			// — issuing it a fresh cookie there would be a pointless side effect (and
			// would never help, since the submitted field can't match a token the
			// client never had).
			if token == "" && csrfSafeMethod(r.Method) {
				var err error
				token, err = generateCSRFToken()
				if err != nil {
					http.Error(w, "csrf token generation failed", http.StatusInternalServerError)
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     csrfCookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					Secure:   forceSecure || r.TLS != nil,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   int((365 * 24 * time.Hour).Seconds()),
				})
			}

			if !csrfSafeMethod(r.Method) {
				// The token alphabet is URL-safe base64 (base64.RawURLEncoding → [A-Za-z0-9-_]),
				// so r.FormValue's percent-decoding is a no-op vs the raw cookie value.
				// Any future encoding change (e.g. standard base64 with +/=) must reconsider
				// this comparison path, since form encoding would transform those characters.
				submitted := r.FormValue(csrfFieldName)
				if submitted == "" || subtle.ConstantTimeCompare([]byte(submitted), []byte(token)) != 1 {
					http.Error(w, "invalid CSRF token", http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfCtxKey{}, token)))
		})
	}
}

func csrfSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}
