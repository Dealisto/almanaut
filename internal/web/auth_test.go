package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func authReach(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestBasicAuthNoHeaderReturns401(t *testing.T) {
	var reached bool
	h := basicAuth("admin", "secret")(authReach(&reached))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if reached {
		t.Fatal("handler must not be reached without credentials")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="Almanaut"` {
		t.Fatalf("WWW-Authenticate = %q, want Basic realm=\"Almanaut\"", got)
	}
}

func TestBasicAuthCorrectCredentialsAllow(t *testing.T) {
	var reached bool
	h := basicAuth("admin", "secret")(authReach(&reached))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !reached || rec.Code != http.StatusOK {
		t.Fatalf("correct credentials must pass: reached=%v code=%d", reached, rec.Code)
	}
}

func TestBasicAuthWrongCredentialsReturn401(t *testing.T) {
	cases := []struct{ name, user, pass string }{
		{"wrong user", "nope", "secret"},
		{"wrong pass", "admin", "nope"},
		{"both wrong", "nope", "nope"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var reached bool
			h := basicAuth("admin", "secret")(authReach(&reached))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.SetBasicAuth(c.user, c.pass)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if reached {
				t.Fatal("handler must not be reached with wrong credentials")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("code = %d, want 401", rec.Code)
			}
		})
	}
}
