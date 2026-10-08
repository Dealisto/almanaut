package web

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// postAuthForm issues an authenticated, CSRF-valid POST and returns the
// recorder. Named distinctly from server_test.go's postForm (unauthenticated,
// fixed-CSRF-token helper) to avoid a redeclaration in this package.
func postAuthForm(t *testing.T, h http.Handler, session *http.Cookie, path string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	// Fetch a CSRF cookie/token from a GET first.
	getRec := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/users", nil)
	getReq.AddCookie(session)
	h.ServeHTTP(getRec, getReq)
	csrf := csrfCookie(getRec.Result().Cookies())

	form := "&" + csrfFieldName + "=" + csrf.Value
	for k, v := range fields {
		form += "&" + k + "=" + v
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form[1:]))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	req.AddCookie(csrf)
	h.ServeHTTP(rec, req)
	return rec
}

func TestCreateAndListUsers(t *testing.T) {
	h, session := authTestServer(t)
	rec := postAuthForm(t, h, session, "/users", map[string]string{"username": "bob", "password": "password123"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create user code = %d, want 303 (body %s)", rec.Code, rec.Body)
	}
	// List shows bob.
	listRec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.AddCookie(session)
	h.ServeHTTP(listRec, req)
	if !strings.Contains(listRec.Body.String(), "bob") {
		t.Fatalf("user list missing bob: %s", listRec.Body)
	}
}

func TestCreateUserShortPasswordRejected(t *testing.T) {
	h, session := authTestServer(t)
	rec := postAuthForm(t, h, session, "/users", map[string]string{"username": "bob", "password": "short"})
	// Re-renders the form (200) with an error, does not redirect.
	if rec.Code == http.StatusSeeOther {
		t.Fatal("short password must be rejected, not accepted")
	}
	if !strings.Contains(rec.Body.String(), "at least") {
		t.Fatalf("expected password-length error, got %s", rec.Body)
	}
}

func TestCannotDeleteLastUser(t *testing.T) {
	h, session := authTestServer(t)
	// Only the seeded "admin" exists. Find its id via the store through a fresh login is overkill;
	// instead assert the guard: deleting id of the sole user is refused.
	// The seeded admin has the lowest id (1).
	rec := postAuthForm(t, h, session, "/users/"+strconv.Itoa(1)+"/delete", nil)
	if rec.Code == http.StatusSeeOther {
		// A redirect would mean it deleted; verify the user still exists instead.
	}
	// Confirm admin still present.
	listRec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.AddCookie(session)
	h.ServeHTTP(listRec, req)
	if !strings.Contains(listRec.Body.String(), "admin") {
		t.Fatal("last user was deleted despite the guard")
	}
}

func TestResetUserPassword(t *testing.T) {
	h, session := authTestServer(t)
	_ = postAuthForm(t, h, session, "/users", map[string]string{"username": "carol", "password": "password123"})
	// Reset carol's password. Find carol's id by listing.
	// carol is user #2 (admin is #1, ordered by creation).
	rec := postAuthForm(t, h, session, "/users/2/password", map[string]string{"password": "newpassword1"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reset password code = %d, want 303 (body %s)", rec.Code, rec.Body)
	}
}

// TestCreateUserDefaultsToViewer confirms createUser assigns the least
// privileged role when the form omits "role" (the create-user form has no
// role selector yet; PR B adds it). Before A8, an unset role failed
// domain.User.Validate and the create silently re-rendered the form with an
// error instead of creating the account.
func TestCreateUserDefaultsToViewer(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
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

	// Inline login flow (mirrors authTestServer) to obtain a session cookie.
	loginGet := httptest.NewRecorder()
	h.ServeHTTP(loginGet, httptest.NewRequest(http.MethodGet, "/login", nil))
	loginCSRF := csrfCookie(loginGet.Result().Cookies())
	loginForm := strings.NewReader("username=admin&password=password123&" + csrfFieldName + "=" + loginCSRF.Value)
	loginReq := httptest.NewRequest(http.MethodPost, "/login", loginForm)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.AddCookie(loginCSRF)
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("login code = %d, want 303 (body %s)", loginRec.Code, loginRec.Body)
	}
	session := sessionCookie(t, loginRec.Result().Cookies())

	rec := postAuthForm(t, h, session, "/users", map[string]string{"username": "newbie", "password": "password123"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create user = %d, want 303 (body %s)", rec.Code, rec.Body)
	}
	u, err := users.GetByUsername("newbie")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if u.Role != domain.RoleViewer {
		t.Fatalf("default role = %q, want viewer", u.Role)
	}
}

func TestSelfChangePassword(t *testing.T) {
	h, session := authTestServer(t)
	rec := postAuthForm(t, h, session, "/account/password", map[string]string{
		"current_password": "password123", "new_password": "brandnewpass",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusSeeOther {
		t.Fatalf("change password code = %d (body %s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "incorrect") {
		t.Fatalf("correct current password reported incorrect: %s", rec.Body)
	}
}

// TestChangePasswordRevokesOtherSessions: a session stolen before a password
// change must not survive it, while the session that made the change stays.
func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	h, db := totpTestHandler(t)
	current := loginAs(t, h, "admin", "password123")
	other := loginAs(t, h, "admin", "password123")

	rec := postCSRF(t, h, current, "/account/password", "current_password=password123&new_password=newpassword1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "password updated") {
		t.Fatalf("change password = %d (body %s)", rec.Code, rec.Body)
	}
	if sessionAlive(t, h, other) {
		t.Error("another session survived the password change")
	}
	if !sessionAlive(t, h, current) {
		t.Error("the session that changed the password was ended")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_events WHERE event_type=? AND user_id=1`, domain.AuthSessionRevoked).Scan(&n); err != nil {
		t.Fatalf("count auth events: %v", err)
	}
	if n != 1 {
		t.Errorf("session_revoked events = %d, want 1", n)
	}
}

func TestResetUserPasswordRevokesSessions(t *testing.T) {
	h, db := totpTestHandler(t)
	admin := loginAs(t, h, "admin", "password123")
	bob := seedUserAndLogin(t, h, db, "bob", domain.RoleEditor) // user id 2

	if rec := postCSRF(t, h, admin, "/users/2/password", "password=newpassword1"); rec.Code != http.StatusSeeOther {
		t.Fatalf("reset password = %d (body %s)", rec.Code, rec.Body)
	}
	if sessionAlive(t, h, bob) {
		t.Error("bob's session survived an admin password reset")
	}
	if rec := postCSRF(t, h, admin, "/users/999/password", "password=newpassword1"); rec.Code != http.StatusNotFound {
		t.Errorf("reset unknown user = %d, want 404", rec.Code)
	}
}

// TestCannotRemoveLastAdmin: demoting or deleting the only admin would leave
// nobody able to manage users, even when other accounts exist.
func TestCannotRemoveLastAdmin(t *testing.T) {
	h, db := totpTestHandler(t)
	admin := loginAs(t, h, "admin", "password123")
	seedUserAndLogin(t, h, db, "vic", domain.RoleViewer) // user id 2
	users := store.NewUserRepo(db)

	rec := postCSRF(t, h, admin, "/users/1/role", "role=viewer")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), errLastAdmin.Error()) {
		t.Fatalf("demote last admin = %d, want the form re-rendered with an error", rec.Code)
	}
	rec = postCSRF(t, h, admin, "/users/1/delete", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), errLastAdmin.Error()) {
		t.Fatalf("delete last admin = %d, want the form re-rendered with an error", rec.Code)
	}
	if u, err := users.Get(1); err != nil || u.Role != domain.RoleAdmin {
		t.Fatalf("admin after refused changes = %+v, %v", u, err)
	}

	// With a second admin, demoting the first is allowed.
	if rec := postCSRF(t, h, admin, "/users/2/role", "role=admin"); rec.Code != http.StatusSeeOther {
		t.Fatalf("promote vic = %d", rec.Code)
	}
	if rec := postCSRF(t, h, admin, "/users/1/role", "role=editor"); rec.Code != http.StatusSeeOther {
		t.Fatalf("demote with another admin = %d (body %s)", rec.Code, rec.Body)
	}
}

func TestUserMutationsOnUnknownUserAre404(t *testing.T) {
	h, _ := totpTestHandler(t)
	admin := loginAs(t, h, "admin", "password123")
	for path, body := range map[string]string{
		"/users/999/role":   "role=viewer",
		"/users/999/delete": "",
	} {
		if rec := postCSRF(t, h, admin, path, body); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, rec.Code)
		}
	}
}
