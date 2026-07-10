package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// postCSRF issues an authenticated POST with a valid CSRF token, returning the
// full recorder so the caller can inspect body and cookies.
func postCSRF(t *testing.T, h http.Handler, cookie *http.Cookie, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec0 := httptest.NewRecorder()
	h.ServeHTTP(rec0, withCookie(httptest.NewRequest(http.MethodGet, "/", nil), cookie))
	csrf := csrfCookie(rec0.Result().Cookies())
	form := strings.NewReader(body + "&" + csrfFieldName + "=" + csrf.Value)
	req := httptest.NewRequest(http.MethodPost, path, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	req.AddCookie(csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// enroll2FA sets up and confirms 2FA for the logged-in session, returning the
// TOTP secret and the recovery codes shown once.
func enroll2FA(t *testing.T, h http.Handler, db *sql.DB, session *http.Cookie, userID int64) (string, []string) {
	t.Helper()
	if rec := postCSRF(t, h, session, "/account/2fa/setup", ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("2fa setup = %d", rec.Code)
	}
	var secret string
	if err := db.QueryRow(`SELECT secret FROM user_totp WHERE user_id=?`, userID).Scan(&secret); err != nil {
		t.Fatalf("read secret: %v", err)
	}
	code, _ := domain.TOTPCode(secret, time.Now().UTC())
	rec := postCSRF(t, h, session, "/account/2fa/confirm", "code="+code)
	if rec.Code != http.StatusOK {
		t.Fatalf("2fa confirm = %d (body %s)", rec.Code, rec.Body)
	}
	var codes []string
	for _, m := range regexp.MustCompile(`<code>([a-z2-7]{5}-[a-z2-7]{5})</code>`).FindAllStringSubmatch(rec.Body.String(), -1) {
		codes = append(codes, m[1])
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("got %d recovery codes, want %d:\n%s", len(codes), recoveryCodeCount, rec.Body)
	}
	var enabled int
	db.QueryRow(`SELECT enabled FROM user_totp WHERE user_id=?`, userID).Scan(&enabled)
	if enabled != 1 {
		t.Fatalf("2fa not enabled after confirm")
	}
	return secret, codes
}

// passwordLogin performs the first login step and returns the recorder.
func passwordLogin(t *testing.T, h http.Handler, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	rec0 := httptest.NewRecorder()
	h.ServeHTTP(rec0, httptest.NewRequest(http.MethodGet, "/login", nil))
	csrf := csrfCookie(rec0.Result().Cookies())
	form := strings.NewReader("username=" + username + "&password=" + password + "&" + csrfFieldName + "=" + csrf.Value)
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cookieNamed(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// submit2FA posts the login challenge with the pending cookie and returns the recorder.
func submit2FA(t *testing.T, h http.Handler, pending *http.Cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec0 := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/login/2fa", nil)
	getReq.AddCookie(pending)
	h.ServeHTTP(rec0, getReq)
	csrf := csrfCookie(rec0.Result().Cookies())
	form := strings.NewReader(body + "&" + csrfFieldName + "=" + csrf.Value)
	req := httptest.NewRequest(http.MethodPost, "/login/2fa", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(pending)
	req.AddCookie(csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func totpTestHandler(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db := rbacDB(t)
	if err := BootstrapAdmin(store.NewUserRepo(db), testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	return newAuthedTestHandler(t, db), db
}

func TestTOTPEnrollAndLogin(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123") // admin is user id 1
	secret, recovery := enroll2FA(t, h, db, session, 1)

	// A fresh password login now demands a second factor instead of a session.
	rec := passwordLogin(t, h, "admin", "password123")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login/2fa?next=%2F" {
		t.Fatalf("password step = %d loc=%q, want 303 to /login/2fa", rec.Code, rec.Header().Get("Location"))
	}
	pending := cookieNamed(rec.Result().Cookies(), pending2FACookieName)
	if pending == nil || pending.Value == "" {
		t.Fatal("no pending 2FA cookie set")
	}
	if cookieValue(rec, sessionCookieName) != "" {
		t.Fatal("session cookie must not be set before 2FA")
	}

	// Wrong code is rejected.
	if rec := submit2FA(t, h, pending, "code=000000"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code = %d, want 401", rec.Code)
	}

	// Correct code completes login.
	code, _ := domain.TOTPCode(secret, time.Now().UTC())
	rec = submit2FA(t, h, pending, "code="+code)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid code = %d, want 303 (body %s)", rec.Code, rec.Body)
	}
	if cookieValue(rec, sessionCookieName) == "" {
		t.Fatal("session cookie not set after successful 2FA")
	}
	_ = recovery
}

func TestTOTPRecoveryCodeLogin(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	_, recovery := enroll2FA(t, h, db, session, 1)

	// Log in with a recovery code.
	pending := cookieNamed(passwordLogin(t, h, "admin", "password123").Result().Cookies(), pending2FACookieName)
	rec := submit2FA(t, h, pending, "recovery="+recovery[0])
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("recovery login = %d, want 303 (body %s)", rec.Code, rec.Body)
	}

	// The same recovery code cannot be reused.
	pending2 := cookieNamed(passwordLogin(t, h, "admin", "password123").Result().Cookies(), pending2FACookieName)
	if rec := submit2FA(t, h, pending2, "recovery="+recovery[0]); rec.Code != http.StatusUnauthorized {
		t.Errorf("reused recovery code = %d, want 401", rec.Code)
	}
}

func TestTOTPAdminReset(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	enroll2FA(t, h, db, session, 1)

	// Admin resets user 1's 2FA.
	if rec := postCSRF(t, h, session, "/users/1/2fa/reset", ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("2fa reset = %d", rec.Code)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM user_totp WHERE user_id=1`).Scan(&n)
	if n != 0 {
		t.Errorf("2fa still present after reset: %d", n)
	}
	// After reset, password login no longer demands 2FA.
	if rec := passwordLogin(t, h, "admin", "password123"); rec.Header().Get("Location") == "/login/2fa?next=%2F" {
		t.Error("login still demands 2FA after reset")
	}
}

func TestTOTPViewerCannotReset(t *testing.T) {
	h, db := totpTestHandler(t)
	viewer := seedUserAndLogin(t, h, db, "vic", domain.RoleViewer)
	if rec := postCSRF(t, h, viewer, "/users/1/2fa/reset", ""); rec.Code != http.StatusForbidden {
		t.Errorf("viewer 2fa reset = %d, want 403", rec.Code)
	}
}
