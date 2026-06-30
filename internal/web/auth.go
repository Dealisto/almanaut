package web

import (
	"crypto/subtle"
	"net/http"
)

// basicAuth returns middleware requiring the given HTTP Basic credentials,
// compared in constant time. New registers it only when both user and pass are
// non-empty, so it never runs in the no-auth default.
func basicAuth(user, pass string) func(http.Handler) http.Handler {
	expUser := []byte(user)
	expPass := []byte(pass)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			// Compute both comparisons unconditionally so timing does not reveal
			// which half (if either) was wrong.
			userOK := subtle.ConstantTimeCompare([]byte(u), expUser) == 1
			passOK := subtle.ConstantTimeCompare([]byte(p), expPass) == 1
			if !ok || !userOK || !passOK {
				w.Header().Set("WWW-Authenticate", `Basic realm="Almanaut"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
