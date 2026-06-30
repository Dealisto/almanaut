package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// passThrough records whether the wrapped handler was reached.
func passThrough(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestCSRFGetSetsCookieAndAllows(t *testing.T) {
	var reached bool
	h := csrfProtect(passThrough(&reached))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hosts", nil))

	if !reached {
		t.Fatal("GET must reach the handler")
	}
	var got string
	for _, c := range rec.Result().Cookies() {
		if c.Name == csrfCookieName {
			got = c.Value
		}
	}
	if got == "" {
		t.Fatal("GET must Set-Cookie csrf_token")
	}
}

func TestCSRFPostMatchingTokenAllows(t *testing.T) {
	var reached bool
	h := csrfProtect(passThrough(&reached))

	form := strings.NewReader(csrfFieldName + "=tok123")
	req := httptest.NewRequest(http.MethodPost, "/hosts", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok123"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !reached || rec.Code != http.StatusOK {
		t.Fatalf("matching token must pass: reached=%v code=%d", reached, rec.Code)
	}
}

func TestCSRFPostMismatchOrMissingRejected(t *testing.T) {
	cases := []struct {
		name      string
		field     string
		cookie    string
		setCookie bool
	}{
		{"mismatch", "a", "b", true},
		{"missing field", "", "b", true},
		{"missing cookie", "a", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var reached bool
			h := csrfProtect(passThrough(&reached))
			req := httptest.NewRequest(http.MethodPost, "/hosts",
				strings.NewReader(csrfFieldName+"="+c.field))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if c.setCookie {
				req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: c.cookie})
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if reached {
				t.Fatal("handler must not be reached on CSRF failure")
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("code = %d, want 403", rec.Code)
			}
		})
	}
}

func TestCSRFTokenFromContextIsSet(t *testing.T) {
	var seen string
	h := csrfProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = csrfTokenFrom(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if seen == "" {
		t.Fatal("csrfTokenFrom(ctx) must return the per-request token inside the chain")
	}
}
