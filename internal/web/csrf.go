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
func csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if c, err := r.Cookie(csrfCookieName); err == nil {
			token = c.Value
		}
		if token == "" {
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
				SameSite: http.SameSiteLaxMode,
				MaxAge:   int((365 * 24 * time.Hour).Seconds()),
			})
		}

		if !csrfSafeMethod(r.Method) {
			submitted := r.FormValue(csrfFieldName)
			if submitted == "" || subtle.ConstantTimeCompare([]byte(submitted), []byte(token)) != 1 {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfCtxKey{}, token)))
	})
}

func csrfSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}
