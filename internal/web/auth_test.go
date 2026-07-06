package web

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("password stored in plaintext")
	}
	if !verifyPassword(hash, "correct horse battery staple") {
		t.Fatal("correct password rejected")
	}
	if verifyPassword(hash, "wrong") {
		t.Fatal("wrong password accepted")
	}
}

func TestNewSessionTokenIsRandomAndHashable(t *testing.T) {
	a, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken: %v", err)
	}
	b, _ := newSessionToken()
	if a == "" || a == b {
		t.Fatalf("tokens not unique: %q %q", a, b)
	}
	if hashToken(a) == a || len(hashToken(a)) != 64 {
		t.Fatalf("hashToken should be a 64-char sha256 hex, got %q", hashToken(a))
	}
	if hashToken(a) != hashToken(a) {
		t.Fatal("hashToken not deterministic")
	}
}

func TestUserContextRoundTrip(t *testing.T) {
	ctx := withUser(context.Background(), domain.User{ID: 1, Username: "alice"})
	u, ok := userFrom(ctx)
	if !ok || u.Username != "alice" {
		t.Fatalf("userFrom = %+v, %v", u, ok)
	}
	if _, ok := userFrom(context.Background()); ok {
		t.Fatal("empty context must report no user")
	}
}

func TestActorReturnsSessionUsername(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(withUser(req.Context(), domain.User{Username: "bob"}))
	if got := actor(req); got != "bob" {
		t.Fatalf("actor = %q, want bob", got)
	}
	// No user in context → empty actor (unchanged legacy behaviour).
	if got := actor(httptest.NewRequest(http.MethodGet, "/", nil)); got != "" {
		t.Fatalf("actor without user = %q, want empty", got)
	}
}

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"/hosts":        "/hosts",
		"/hosts?q=1":    "/hosts?q=1",
		"":              "/",
		"//evil.com":    "/",
		"https://x.com": "/",
		"javascript:1":  "/",
		"/\\evil.com":   "/",
		"/\\/evil.com":  "/",
		"/ok/path":      "/ok/path",
	}
	for in, want := range cases {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func testLogger() *log.Logger { return log.New(io.Discard, "", 0) }

func csrfCookie(cookies []*http.Cookie) *http.Cookie {
	for _, c := range cookies {
		if c.Name == csrfCookieName {
			return c
		}
	}
	return &http.Cookie{Name: csrfCookieName}
}

func sessionCookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatal("no session cookie set by login")
	return nil
}

// authTestServer builds a server with auth enabled and one seeded user, and
// returns the handler plus a valid session cookie for that user.
func authTestServer(t *testing.T) (http.Handler, *http.Cookie) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	users := store.NewUserRepo(db)
	if err := BootstrapAdmin(users, testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	h := newAuthedTestHandler(t, db)

	// Log in through the real handler to obtain a session cookie.
	rec := httptest.NewRecorder()
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	csrf := csrfCookie(rec.Result().Cookies())
	form := strings.NewReader("username=admin&password=password123&" + csrfFieldName + "=" + csrf.Value)
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusSeeOther {
		t.Fatalf("login POST code = %d, want 303 (body: %s)", rec2.Code, rec2.Body)
	}
	return h, sessionCookie(t, rec2.Result().Cookies())
}

func TestSessionAuthRedirectsUnauthenticatedPage(t *testing.T) {
	h, _ := authTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hosts", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303 redirect to /login", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/login") {
		t.Fatalf("Location = %q, want /login...", loc)
	}
}

func TestSessionAuthUnauthenticatedAPIis401(t *testing.T) {
	h, _ := authTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/hosts", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
}

func TestSessionAuthValidSessionPasses(t *testing.T) {
	h, cookie := authTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/hosts", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated /hosts code = %d, want 200", rec.Code)
	}
}
