package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestTOTPDisableRateLimitIsPerUserAndSeparateFromLogin(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	_, recovery := enroll2FA(t, h, db, session, 1)
	otherSession := seedUserAndLogin(t, h, db, "other", domain.RoleViewer)
	other, err := store.NewUserRepo(db).GetByUsername("other")
	if err != nil {
		t.Fatal(err)
	}
	_, otherRecovery := enroll2FA(t, h, db, otherSession, other.ID)
	for i := 0; i < loginFailureThreshold; i++ {
		if rec := postCSRF(t, h, session, "/account/2fa/disable", "code=invalid"); rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid attempt %d = %d, want 400", i+1, rec.Code)
		}
	}
	if rec := postCSRF(t, h, session, "/account/2fa/disable", "recovery="+recovery[0]); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt during lockout = %d, want 429", rec.Code)
	}
	if left, err := store.NewTOTPRepo(db).CountUnusedRecovery(1); err != nil || left != recoveryCodeCount {
		t.Fatalf("locked request consumed a code: left=%d err=%v", left, err)
	}
	if rec := postCSRF(t, h, otherSession, "/account/2fa/disable", "recovery="+otherRecovery[0]); rec.Code != http.StatusSeeOther {
		t.Fatalf("another user's disable = %d, want 303", rec.Code)
	}
	pending := cookieNamed(passwordLogin(t, h, "admin", "password123").Result().Cookies(), pending2FACookieName)
	if pending == nil {
		t.Fatal("disable lockout must not block password login")
	}
	if rec := submit2FA(t, h, pending, "recovery="+recovery[0]); rec.Code != http.StatusSeeOther {
		t.Fatalf("login challenge during disable lockout = %d, want 303", rec.Code)
	}
}

func TestTOTPDisableFailurePreservesSecondFactor(t *testing.T) {
	for _, recoveryFactor := range []bool{false, true} {
		name := "authenticator"
		if recoveryFactor {
			name = "recovery"
		}
		t.Run(name, func(t *testing.T) {
			h, db := totpTestHandler(t)
			session := loginAs(t, h, "admin", "password123")
			secret, recovery := enroll2FA(t, h, db, session, 1)
			step := lastUsedStep(t, db, 1)
			body := "code=" + codeAtStep(t, secret, step+1)
			if recoveryFactor {
				body = "recovery=" + recovery[0]
			}
			if _, err := db.Exec(`CREATE TRIGGER fail_disable BEFORE DELETE ON user_totp BEGIN SELECT RAISE(ABORT, 'disable unavailable'); END`); err != nil {
				t.Fatal(err)
			}
			if rec := postCSRF(t, h, session, "/account/2fa/disable", body); rec.Code != http.StatusInternalServerError {
				t.Fatalf("failed disable = %d, want 500", rec.Code)
			}
			factor, err := store.NewTOTPRepo(db).Get(1)
			if err != nil || !factor.Enabled || factor.Secret != secret {
				t.Fatalf("failed disable changed active factor: %+v, %v", factor, err)
			}
			if got := lastUsedStep(t, db, 1); got != step {
				t.Errorf("failed disable consumed TOTP step: got %d, want %d", got, step)
			}
			if left, err := store.NewTOTPRepo(db).CountUnusedRecovery(1); err != nil || left != recoveryCodeCount {
				t.Errorf("failed disable consumed recovery code: left=%d err=%v", left, err)
			}
			if _, err := db.Exec(`DROP TRIGGER fail_disable`); err != nil {
				t.Fatal(err)
			}
			if rec := postCSRF(t, h, session, "/account/2fa/disable", body); rec.Code != http.StatusSeeOther {
				t.Fatalf("retry with original code = %d, want 303", rec.Code)
			}
			if _, err := store.NewTOTPRepo(db).Get(1); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("factor remains after successful disable: %v", err)
			}
		})
	}
}

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
	if err := BootstrapAdmin(db, testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	return newAuthedTestHandler(t, db), db
}

func TestTOTPInvalidCodesKeepAnAccessibleRetryForm(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	postCSRF(t, h, session, "/account/2fa/setup", "")
	before, err := store.NewTOTPRepo(db).Get(1)
	if err != nil {
		t.Fatal(err)
	}
	rec := postCSRF(t, h, session, "/account/2fa/confirm", "code=invalid")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("invalid enrollment response: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{`role="alert"`, `aria-describedby="two-factor-error"`, `action="/account/2fa/confirm"`, "Invalid code"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("enrollment retry missing %q", want)
		}
	}
	after, err := store.NewTOTPRepo(db).Get(1)
	if err != nil || after.Enabled || after.Secret != before.Secret {
		t.Fatal("invalid enrollment changed the stored factor")
	}
	enroll2FA(t, h, db, session, 1)
	rec = postCSRF(t, h, session, "/account/2fa/disable", "code=invalid")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `action="/account/2fa/disable"`) || !strings.Contains(rec.Body.String(), `role="alert"`) {
		t.Fatal("invalid disable must show the error and retry form")
	}
	after, err = store.NewTOTPRepo(db).Get(1)
	if err != nil || !after.Enabled {
		t.Fatal("invalid disable changed the stored factor")
	}
	if left, err := store.NewTOTPRepo(db).CountUnusedRecovery(1); err != nil || left != recoveryCodeCount {
		t.Fatal("invalid disable consumed recovery codes")
	}
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

func TestTOTPConfirmEnabledFactorDoesNotExposeSecretsOrReplaceRecovery(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	secret, recovery := enroll2FA(t, h, db, session, 1)
	usedStep := lastUsedStep(t, db, 1)
	for _, body := range []string{"code=invalid", "code=" + codeAtStep(t, secret, usedStep)} {
		rec := postCSRF(t, h, session, "/account/2fa/confirm", body)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/2fa" {
			t.Errorf("repeat confirmation = %d, want redirect to account 2FA", rec.Code)
		}
		if strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Body.String(), "otpauth:") || strings.Contains(rec.Body.String(), "<code>") {
			t.Error("repeat confirmation exposed factor or recovery secrets")
		}
	}
	if lastUsedStep(t, db, 1) != usedStep {
		t.Error("repeat confirmation changed the replay marker")
	}
	for _, code := range recovery {
		ok, err := store.NewTOTPRepo(db).UseRecoveryCode(1, hashToken(domain.NormalizeRecoveryCode(code)))
		if err != nil || !ok {
			t.Fatalf("original recovery code was replaced: accepted=%v err=%v", ok, err)
		}
	}
}

func TestTOTPConcurrentConfirmationReturnsRecoveryCodesOnce(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	if rec := postCSRF(t, h, session, "/account/2fa/setup", ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("setup = %d", rec.Code)
	}
	totp := store.NewTOTPRepo(db)
	pending, err := totp.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	code, err := domain.TOTPCode(pending.Secret, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	handler := confirm2FA(totp, db)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/account/2fa/confirm", strings.NewReader("code="+code))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req = req.WithContext(withUser(req.Context(), domain.User{ID: 1, Username: "admin", Role: domain.RoleAdmin}))
			rec := httptest.NewRecorder()
			<-start
			handler.ServeHTTP(rec, req)
			results <- rec
		}()
	}
	close(start)
	statuses := map[int]int{}
	for range 2 {
		rec := <-results
		statuses[rec.Code]++
		if rec.Code == http.StatusOK {
			matches := regexp.MustCompile(`<code>([a-z2-7]{5}-[a-z2-7]{5})</code>`).FindAllStringSubmatch(rec.Body.String(), -1)
			if len(matches) != recoveryCodeCount {
				t.Fatalf("returned %d recovery codes", len(matches))
			}
			for _, match := range matches {
				ok, err := totp.UseRecoveryCode(1, hashToken(domain.NormalizeRecoveryCode(match[1])))
				if err != nil || !ok {
					t.Fatalf("returned recovery code was replaced: accepted=%v err=%v", ok, err)
				}
			}
		}
	}
	if statuses[http.StatusOK] != 1 || statuses[http.StatusSeeOther] != 1 {
		t.Fatalf("concurrent confirmation statuses = %v", statuses)
	}
}

func TestTOTPLoginRollsBackOnPersistenceFailure(t *testing.T) {
	for _, table := range []string{"sessions", "totp_pending"} {
		t.Run(table, func(t *testing.T) {
			h, db := totpTestHandler(t)
			session := loginAs(t, h, "admin", "password123")
			_, recovery := enroll2FA(t, h, db, session, 1)
			pending := cookieNamed(passwordLogin(t, h, "admin", "password123").Result().Cookies(), pending2FACookieName)
			operation := "INSERT"
			if table == "totp_pending" {
				operation = "DELETE"
			}
			if _, err := db.Exec("CREATE TRIGGER fail_login BEFORE " + operation + " ON " + table + " BEGIN SELECT RAISE(ABORT, 'injected failure'); END"); err != nil {
				t.Fatal(err)
			}
			rec := submit2FA(t, h, pending, "recovery="+recovery[0])
			if rec.Code != http.StatusInternalServerError || cookieValue(rec, sessionCookieName) != "" {
				t.Errorf("failed login = %d, session=%q", rec.Code, cookieValue(rec, sessionCookieName))
			}
			if left, err := store.NewTOTPRepo(db).CountUnusedRecovery(1); err != nil || left != recoveryCodeCount {
				t.Errorf("failed login consumed recovery code: remaining=%d err=%v", left, err)
			}
			if _, err := store.NewTOTPRepo(db).PendingUser(hashToken(pending.Value), nowRFC3339()); err != nil {
				t.Errorf("failed login consumed challenge: %v", err)
			}
			var count int
			if err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 1 {
				t.Errorf("failed login created session: count=%d err=%v", count, err)
			}
			if _, err := db.Exec("DROP TRIGGER fail_login"); err != nil {
				t.Fatal(err)
			}
			rec = submit2FA(t, h, pending, "recovery="+recovery[0])
			if rec.Code != http.StatusSeeOther || cookieValue(rec, sessionCookieName) == "" {
				t.Fatalf("retry after persistence recovery failed: %d %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestTOTPConcurrentLoginConsumesChallengeOnce(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	_, recovery := enroll2FA(t, h, db, session, 1)
	pending := cookieNamed(passwordLogin(t, h, "admin", "password123").Result().Cookies(), pending2FACookieName)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for i := range 2 {
		go func() {
			body := "recovery=" + recovery[i] + "&" + csrfFieldName + "=concurrent-test"
			req := httptest.NewRequest(http.MethodPost, "/login/2fa", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(pending)
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "concurrent-test"})
			rec := httptest.NewRecorder()
			<-start
			h.ServeHTTP(rec, req)
			results <- rec
		}()
	}
	close(start)
	locations := map[string]int{}
	sessionCookies := 0
	for range 2 {
		rec := <-results
		if rec.Code != http.StatusSeeOther {
			t.Errorf("concurrent login = %d: %s", rec.Code, rec.Body)
		}
		locations[rec.Header().Get("Location")]++
		if cookieValue(rec, sessionCookieName) != "" {
			sessionCookies++
		}
	}
	if locations["/"] != 1 || locations["/login"] != 1 || sessionCookies != 1 {
		t.Fatalf("challenge reused: locations=%v session cookies=%d", locations, sessionCookies)
	}
	if left, err := store.NewTOTPRepo(db).CountUnusedRecovery(1); err != nil || left != recoveryCodeCount-1 {
		t.Fatalf("expected only one recovery code consumed: remaining=%d err=%v", left, err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil || count != 2 {
		t.Fatalf("expected original session and one new session: count=%d err=%v", count, err)
	}
}
