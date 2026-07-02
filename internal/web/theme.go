package web

import "net/http"

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
