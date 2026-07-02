package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestThemeFromCookie(t *testing.T) {
	cases := []struct {
		name, cookie, want string
		set                bool
	}{
		{name: "absent", set: false, want: "system"},
		{name: "system", cookie: "system", set: true, want: "system"},
		{name: "light", cookie: "light", set: true, want: "light"},
		{name: "dark", cookie: "dark", set: true, want: "dark"},
		{name: "invalid", cookie: "blue", set: true, want: "system"},
		{name: "empty", cookie: "", set: true, want: "system"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if c.set {
				req.AddCookie(&http.Cookie{Name: themeCookieName, Value: c.cookie})
			}
			if got := themeFromCookie(req); got != c.want {
				t.Errorf("themeFromCookie(%q) = %q, want %q", c.cookie, got, c.want)
			}
		})
	}
}
