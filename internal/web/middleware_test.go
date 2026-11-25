package web

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"

	"github.com/go-chi/chi/v5/middleware"
)

func TestSecurityHeadersSet(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'", "form-action 'self'", "object-src 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q missing %q", csp, want)
		}
	}
}

// Headers must be applied through the full middleware stack, including on the
// unauthenticated /healthz endpoint.
func TestSecurityHeadersWiredOnServer(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/hosts", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("GET %s: missing nosniff header", path)
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("GET %s: missing Content-Security-Policy header", path)
		}
	}
}

func TestRecovererReturns500AndLogsPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	h := recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "panic: boom") {
		t.Fatalf("log = %q, want it to contain \"panic: boom\"", buf.String())
	}
}

func TestRecovererRepanicsErrAbortHandler(t *testing.T) {
	logger := log.New(&bytes.Buffer{}, "", 0)
	h := recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler to propagate", rec)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRequestLoggerLogsAndPreservesResponse(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	h := requestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hi"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hosts", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418 (response must be untouched)", rec.Code)
	}
	if rec.Body.String() != "hi" {
		t.Fatalf("body = %q, want \"hi\"", rec.Body.String())
	}
	line := buf.String()
	for _, want := range []string{"GET", "/hosts", "418"} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line %q missing %q", line, want)
		}
	}
}

func TestRequestLoggerSetsRequestIDHeaderFromContext(t *testing.T) {
	logger := log.New(&bytes.Buffer{}, "", 0)
	// chi's RequestID middleware populates the context; wrap requestLogger inside it.
	h := middleware.RequestID(requestLogger(logger)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {},
	)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("X-Request-Id response header is empty, want a generated id")
	}
}

func TestSensitiveResponsesAreNotCached(t *testing.T) {
	h, db := totpTestHandler(t)
	check := func(label string, rec *httptest.ResponseRecorder, status int) {
		t.Helper()
		if rec.Code != status {
			t.Fatalf("%s status = %d, want %d", label, rec.Code, status)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q, want no-store", label, got)
		}
	}
	get := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	check("login form", get("/login", nil), http.StatusOK)
	check("unauthenticated redirect", get("/", nil), http.StatusSeeOther)
	check("unauthenticated API", get("/api/hosts", nil), http.StatusUnauthorized)
	session := loginAs(t, h, "admin", "password123")
	for _, path := range []string{"/", "/account/tokens", "/account/2fa", "/api/hosts"} {
		check(path, get(path, session), http.StatusOK)
	}
	check("new token", postCSRF(t, h, session, "/account/tokens", "label=cache-test"), http.StatusOK)
	check("TOTP setup", postCSRF(t, h, session, "/account/2fa/setup", ""), http.StatusSeeOther)
	check("TOTP secret", get("/account/2fa", session), http.StatusOK)
	pending, err := store.NewTOTPRepo(db).Get(1)
	if err != nil {
		t.Fatal(err)
	}
	code, err := domain.TOTPCode(pending.Secret, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	check("TOTP recovery codes", postCSRF(t, h, session, "/account/2fa/confirm", "code="+code), http.StatusOK)
	check("logout", postCSRF(t, h, session, "/logout", ""), http.StatusSeeOther)
	check("after logout", get("/", session), http.StatusSeeOther)
	for _, path := range []string{"/static/app.css", "/static/app.js"} {
		rec := get(path, nil)
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("ETag") == "" {
			t.Errorf("%s lost static revalidation headers: status=%d headers=%v", path, rec.Code, rec.Header())
		}
	}
}
