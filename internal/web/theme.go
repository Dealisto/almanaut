package web

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// themeCookieName holds the persisted UI theme choice.
const themeCookieName = "theme"

// themeFromCookie returns the user's theme preference: "system", "light", or
// "dark". It defaults to "system" when the cookie is absent or holds an
// unrecognized value, so a stale or tampered cookie degrades to OS-follow.
func themeFromCookie(r *http.Request) string {
	c, err := r.Cookie(themeCookieName)
	if err != nil {
		return "system"
	}
	switch c.Value {
	case "light", "dark", "system":
		return c.Value
	default:
		return "system"
	}
}

// safeRedirectTarget returns a local path to redirect back to after a theme
// change: the Referer's path (with query) when the Referer is same-host and
// absolute, otherwise "/". This keeps the user on their page without allowing
// an off-site open redirect.
func safeRedirectTarget(r *http.Request) string {
	ref := r.Header.Get("Referer")
	if ref == "" {
		return "/"
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "/"
	}
	if u.Host != "" && u.Host != r.Host {
		return "/"
	}
	if !strings.HasPrefix(u.Path, "/") {
		return "/"
	}
	if u.RawQuery != "" {
		return u.Path + "?" + u.RawQuery
	}
	return u.Path
}

// setTheme persists the UI theme choice in a cookie and redirects back. An
// unrecognized value resets to "system". forceSecure mirrors the CSRF cookie's
// Secure handling (set from cfg.SecureCookies).
func setTheme(forceSecure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := r.FormValue("theme")
		if v != "light" && v != "dark" {
			v = "system"
		}
		http.SetCookie(w, &http.Cookie{
			Name:     themeCookieName,
			Value:    v,
			Path:     "/",
			HttpOnly: true,
			Secure:   forceSecure || r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int((365 * 24 * time.Hour).Seconds()),
		})
		http.Redirect(w, r, safeRedirectTarget(r), http.StatusSeeOther)
	}
}
