package web

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
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
	h := csrfProtect(false)(passThrough(&reached))
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
	h := csrfProtect(false)(passThrough(&reached))

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

func TestCSRFRequestOrigins(t *testing.T) {
	for _, tc := range []struct {
		name, origin, site string
		secure, allowed    bool
	}{
		{name: "same origin", origin: "http://inventory.example", allowed: true},
		{name: "TLS terminated at proxy", origin: "https://inventory.example", secure: true, allowed: true},
		{name: "browser same origin", site: "same-origin", allowed: true},
		{name: "non-browser client", allowed: true},
		{name: "different origin", origin: "https://other.example"},
		{name: "sibling subdomain", origin: "https://other.inventory.example", site: "same-site"},
		{name: "cross site", site: "cross-site"},
		{name: "opaque origin", origin: "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reached bool
			h := csrfProtect(tc.secure)(passThrough(&reached))
			req := httptest.NewRequest(http.MethodPost, "http://inventory.example/hosts", strings.NewReader(csrfFieldName+"=valid-token"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Sec-Fetch-Site", tc.site)
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "valid-token"})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			want := http.StatusForbidden
			if tc.allowed {
				want = http.StatusOK
			}
			if reached != tc.allowed || rec.Code != want {
				t.Fatalf("request origin: reached=%v status=%d, want reached=%v status=%d", reached, rec.Code, tc.allowed, want)
			}
		})
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
			h := csrfProtect(false)(passThrough(&reached))
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

func TestCSRFUnsafeMethodDoesNotSetCookie(t *testing.T) {
	// A POST with no cookie is rejected; it must not mint a fresh token cookie,
	// since the request can never carry a matching token anyway.
	var reached bool
	h := csrfProtect(false)(passThrough(&reached))
	req := httptest.NewRequest(http.MethodPost, "/hosts",
		strings.NewReader(csrfFieldName+"=a"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == csrfCookieName {
			t.Errorf("rejected POST must not Set-Cookie %s, got %q", csrfCookieName, c.Value)
		}
	}
}

func TestCSRFCookieSecureFlag(t *testing.T) {
	cases := []struct {
		name        string
		forceSecure bool
		tls         bool
		wantSecure  bool
	}{
		{"plain http, no force", false, false, false},
		{"plain http, forced (proxy)", true, false, true},
		{"direct tls auto-detected", false, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := csrfProtect(c.forceSecure)(passThrough(new(bool)))
			req := httptest.NewRequest(http.MethodGet, "/hosts", nil)
			if c.tls {
				req.TLS = &tls.ConnectionState{}
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			var cookie *http.Cookie
			for _, ck := range rec.Result().Cookies() {
				if ck.Name == csrfCookieName {
					cookie = ck
				}
			}
			if cookie == nil {
				t.Fatal("GET must Set-Cookie csrf_token")
			}
			if cookie.Secure != c.wantSecure {
				t.Errorf("Secure = %v, want %v", cookie.Secure, c.wantSecure)
			}
		})
	}
}

func TestCSRFTokenFromContextIsSet(t *testing.T) {
	var seen string
	h := csrfProtect(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = csrfTokenFrom(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if seen == "" {
		t.Fatal("csrfTokenFrom(ctx) must return the per-request token inside the chain")
	}
}

func TestCSRFRoundTripGeneratedToken(t *testing.T) {
	// generate a real token and use it as both cookie and form field; the handler must be reached.
	tok, err := generateCSRFToken()
	if err != nil {
		t.Fatalf("generateCSRFToken: %v", err)
	}
	var reached bool
	h := csrfProtect(false)(passThrough(&reached))

	body := strings.NewReader(csrfFieldName + "=" + tok)
	req := httptest.NewRequest(http.MethodPost, "/hosts", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: tok})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !reached || rec.Code != http.StatusOK {
		t.Fatalf("round-trip with generated token must pass: reached=%v code=%d", reached, rec.Code)
	}
}

func TestRenderInjectsCSRFField(t *testing.T) {
	// Build a request whose context carries a known token, render a form page,
	// and assert the hidden field is present with that token.
	r := httptest.NewRequest(http.MethodGet, "/hosts/new", nil)
	r = r.WithContext(context.WithValue(r.Context(), csrfCtxKey{}, "tok-xyz"))
	rec := httptest.NewRecorder()
	render(rec, r, "host_form.html", formData[domain.Host]{
		Title: "New host", Heading: "New host",
		Action: "/hosts", SubmitLabel: "Create",
		Item:   domain.Host{Type: "physical"},
		Extras: map[string]any{"Types": domain.DefaultHostTypes()},
	})
	body := rec.Body.String()
	if !strings.Contains(body, `name="csrf_token"`) || !strings.Contains(body, "tok-xyz") {
		t.Fatalf("rendered form missing csrf field with token; got:\n%s", body)
	}
}
