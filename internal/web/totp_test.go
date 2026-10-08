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

	// The code that confirmed enrollment was consumed there and cannot be
	// replayed to log in.
	step := lastUsedStep(t, db, 1)
	if rec := submit2FA(t, h, pending, "code="+codeAtStep(t, secret, step)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("enrollment code replayed at login = %d, want 401", rec.Code)
	}

	// A code for the next step (within the ±1 skew window) completes login.
	code := codeAtStep(t, secret, step+1)
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

// TestTOTPSetupCannotResetEnabled verifies re-running setup on an already-
// enabled factor does not silently disable it (security-review finding).
func TestTOTPSetupCannotResetEnabled(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	enroll2FA(t, h, db, session, 1)

	// Attempt to re-initialize; must be refused with no change to the secret.
	var before string
	db.QueryRow(`SELECT secret FROM user_totp WHERE user_id=1`).Scan(&before)
	if rec := postCSRF(t, h, session, "/account/2fa/setup", ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("re-setup = %d", rec.Code)
	}
	var after string
	var enabled int
	db.QueryRow(`SELECT secret, enabled FROM user_totp WHERE user_id=1`).Scan(&after, &enabled)
	if enabled != 1 || after != before {
		t.Errorf("re-setup changed factor: enabled=%d secretChanged=%v", enabled, after != before)
	}
	// And login still demands 2FA.
	if rec := passwordLogin(t, h, "admin", "password123"); rec.Header().Get("Location") != "/login/2fa?next=%2F" {
		t.Error("2FA no longer required after re-setup attempt")
	}
}

func TestTOTPViewerCannotReset(t *testing.T) {
	h, db := totpTestHandler(t)
	viewer := seedUserAndLogin(t, h, db, "vic", domain.RoleViewer)
	if rec := postCSRF(t, h, viewer, "/users/1/2fa/reset", ""); rec.Code != http.StatusForbidden {
		t.Errorf("viewer 2fa reset = %d, want 403", rec.Code)
	}
}

// lastUsedStep reads the replay marker of userID's TOTP factor.
func lastUsedStep(t *testing.T, db *sql.DB, userID int64) int64 {
	t.Helper()
	var step int64
	if err := db.QueryRow(`SELECT last_used_step FROM user_totp WHERE user_id=?`, userID).Scan(&step); err != nil {
		t.Fatalf("read last_used_step: %v", err)
	}
	return step
}

// codeAtStep returns secret's code for one exact time step, so a test can name
// the step it means instead of racing the 30s clock boundary.
func codeAtStep(t *testing.T, secret string, step int64) string {
	t.Helper()
	code, err := domain.TOTPCode(secret, time.Unix(step*30, 0))
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}
	return code
}

// sessionAlive reports whether cookie still authenticates a page request.
func sessionAlive(t *testing.T, h http.Handler, cookie *http.Cookie) bool {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/", nil), cookie))
	return rec.Code == http.StatusOK
}

// TestTOTPCodeCannotBeReplayed: a code that completed one login is refused for
// a second login while it is still inside its validity window (RFC 6238 §5.2).
func TestTOTPCodeCannotBeReplayed(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	secret, _ := enroll2FA(t, h, db, session, 1)
	code := codeAtStep(t, secret, lastUsedStep(t, db, 1)+1)

	pending := cookieNamed(passwordLogin(t, h, "admin", "password123").Result().Cookies(), pending2FACookieName)
	if rec := submit2FA(t, h, pending, "code="+code); rec.Code != http.StatusSeeOther {
		t.Fatalf("first use = %d, want 303 (body %s)", rec.Code, rec.Body)
	}
	pending2 := cookieNamed(passwordLogin(t, h, "admin", "password123").Result().Cookies(), pending2FACookieName)
	if rec := submit2FA(t, h, pending2, "code="+code); rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed code = %d, want 401", rec.Code)
	}
}

// TestTOTPReplayedCodeCannotDisable: a code already used to log in cannot be
// reused to turn the second factor off.
func TestTOTPReplayedCodeCannotDisable(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	secret, _ := enroll2FA(t, h, db, session, 1)
	used := codeAtStep(t, secret, lastUsedStep(t, db, 1))

	if rec := postCSRF(t, h, session, "/account/2fa/disable", "code="+used); rec.Code != http.StatusBadRequest {
		t.Fatalf("disable with consumed code = %d, want 400", rec.Code)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM user_totp WHERE user_id=1`).Scan(&n)
	if n != 1 {
		t.Fatal("2FA was disabled with a replayed code")
	}
}

// TestTOTPAdminResetRevokesSessions: the docs promise a 2FA reset ends the
// user's sessions; an admin resetting their own factor keeps the current one.
func TestTOTPAdminResetRevokesSessions(t *testing.T) {
	h, db := totpTestHandler(t)
	admin := loginAs(t, h, "admin", "password123")
	bob := seedUserAndLogin(t, h, db, "bob", domain.RoleEditor) // user id 2
	if !sessionAlive(t, h, bob) {
		t.Fatal("bob's session should work before the reset")
	}

	if rec := postCSRF(t, h, admin, "/users/2/2fa/reset", ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("2fa reset = %d", rec.Code)
	}
	if sessionAlive(t, h, bob) {
		t.Error("bob's session survived an admin 2FA reset")
	}

	if rec := postCSRF(t, h, admin, "/users/1/2fa/reset", ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("self 2fa reset = %d", rec.Code)
	}
	if !sessionAlive(t, h, admin) {
		t.Error("resetting your own 2FA must not end the session you did it from")
	}
}

func TestTOTPAdminResetUnknownUserIs404(t *testing.T) {
	h, _ := totpTestHandler(t)
	admin := loginAs(t, h, "admin", "password123")
	if rec := postCSRF(t, h, admin, "/users/999/2fa/reset", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("reset unknown user = %d, want 404", rec.Code)
	}
}
