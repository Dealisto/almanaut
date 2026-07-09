package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// loginAs performs a real login POST for the given credentials and returns the
// resulting session cookie. Mirrors the login flow inlined in authTestServer,
// but parameterised so tests can log in as a second, non-seeded user.
func loginAs(t *testing.T, h http.Handler, username, password string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	csrf := csrfCookie(rec.Result().Cookies())
	form := strings.NewReader("username=" + username + "&password=" + password + "&" + csrfFieldName + "=" + csrf.Value)
	req := httptest.NewRequest(http.MethodPost, "/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusSeeOther {
		t.Fatalf("login POST for %q code = %d, want 303 (body: %s)", username, rec2.Code, rec2.Body)
	}
	return sessionCookie(t, rec2.Result().Cookies())
}

func TestTokenUICreateShowsRawOnce(t *testing.T) {
	h, session := authTestServer(t)

	// GET the page to obtain a CSRF cookie/token.
	rec := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/account/tokens", nil)
	getReq.AddCookie(session)
	h.ServeHTTP(rec, getReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /account/tokens = %d, want 200", rec.Code)
	}
	csrf := csrfCookie(rec.Result().Cookies())

	// POST to create a token.
	rec = httptest.NewRecorder()
	form := strings.NewReader("label=ci&" + csrfFieldName + "=" + csrf.Value)
	postReq := httptest.NewRequest(http.MethodPost, "/account/tokens", form)
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(session)
	postReq.AddCookie(csrf)
	h.ServeHTTP(rec, postReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /account/tokens = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "alm_") {
		t.Fatalf("created token page does not show the raw alm_ token")
	}

	// The token is not shown again on a fresh GET.
	rec = httptest.NewRecorder()
	getReq = httptest.NewRequest(http.MethodGet, "/account/tokens", nil)
	getReq.AddCookie(session)
	h.ServeHTTP(rec, getReq)
	if strings.Contains(rec.Body.String(), "alm_") {
		t.Fatalf("raw token leaked on subsequent GET")
	}
	if !strings.Contains(rec.Body.String(), "ci") {
		t.Fatalf("token label not listed")
	}
}

// TestTokenCreateDefaultsToReadWriteScope confirms createToken assigns
// read-write when the form omits "scope" (the token form has no scope
// selector yet; PR B adds it), and rejects an explicit but invalid scope.
func TestTokenCreateDefaultsToReadWriteScope(t *testing.T) {
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

	rec := postAuthForm(t, h, session, "/account/tokens", map[string]string{"label": "ci"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create token = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	u, err := users.GetByUsername("admin")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	list, err := store.NewTokenRepo(db).ListByUser(u.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListByUser = %+v, err %v, want 1 token", list, err)
	}
	if list[0].Scope != string(domain.ScopeReadWrite) {
		t.Fatalf("default scope = %q, want read-write", list[0].Scope)
	}

	// An explicit, invalid scope is rejected rather than silently accepted.
	rec = postAuthForm(t, h, session, "/account/tokens", map[string]string{"label": "bad", "scope": "bogus"})
	if strings.Contains(rec.Body.String(), "alm_") {
		t.Fatalf("token created with invalid scope: %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "invalid token scope") {
		t.Fatalf("expected invalid-scope error, got %s", rec.Body)
	}
}

// tokenIDRE extracts the id from a token row's delete-form action
// (/account/tokens/{id}/delete) rendered by templates/tokens.html.
var tokenIDRE = regexp.MustCompile(`/account/tokens/(\d+)/delete`)

func TestTokenUIOwnerScopedRevoke(t *testing.T) {
	h, adminSession := authTestServer(t)

	// Admin creates a second user, "bob", and logs in as them.
	rec := postAuthForm(t, h, adminSession, "/users", map[string]string{"username": "bob", "password": "password123"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create bob code = %d, want 303 (body %s)", rec.Code, rec.Body)
	}
	bobSession := loginAs(t, h, "bob", "password123")

	// Each user creates their own token.
	adminCreate := postAuthForm(t, h, adminSession, "/account/tokens", map[string]string{"label": "admin-token"})
	if adminCreate.Code != http.StatusOK {
		t.Fatalf("admin create token = %d, want 200 (body %s)", adminCreate.Code, adminCreate.Body)
	}
	bobCreate := postAuthForm(t, h, bobSession, "/account/tokens", map[string]string{"label": "bob-token"})
	if bobCreate.Code != http.StatusOK {
		t.Fatalf("bob create token = %d, want 200 (body %s)", bobCreate.Code, bobCreate.Body)
	}

	// Bob's list shows only his own token; extract its id from the delete form.
	bobList := httptest.NewRecorder()
	bobListReq := httptest.NewRequest(http.MethodGet, "/account/tokens", nil)
	bobListReq.AddCookie(bobSession)
	h.ServeHTTP(bobList, bobListReq)
	if strings.Contains(bobList.Body.String(), "admin-token") {
		t.Fatalf("bob's token list leaks admin's token: %s", bobList.Body)
	}
	match := tokenIDRE.FindStringSubmatch(bobList.Body.String())
	if match == nil {
		t.Fatalf("could not find bob's token id in list: %s", bobList.Body)
	}
	bobTokenID := match[1]

	// Admin tries to delete bob's token by id. Delete is scoped to (id, ownerID),
	// so this must be a no-op, not an authorization error leaking existence.
	deleteAsAdmin := postAuthForm(t, h, adminSession, "/account/tokens/"+bobTokenID+"/delete", nil)
	if deleteAsAdmin.Code != http.StatusSeeOther {
		t.Fatalf("admin delete-attempt on bob's token = %d, want 303 (body %s)", deleteAsAdmin.Code, deleteAsAdmin.Body)
	}

	// Bob's token must still be listed.
	bobListAfter := httptest.NewRecorder()
	bobListAfterReq := httptest.NewRequest(http.MethodGet, "/account/tokens", nil)
	bobListAfterReq.AddCookie(bobSession)
	h.ServeHTTP(bobListAfter, bobListAfterReq)
	if !strings.Contains(bobListAfter.Body.String(), "bob-token") {
		t.Fatalf("admin was able to revoke bob's token via another user's session: %s", bobListAfter.Body)
	}

	// Bob can revoke his own token.
	deleteAsBob := postAuthForm(t, h, bobSession, "/account/tokens/"+bobTokenID+"/delete", nil)
	if deleteAsBob.Code != http.StatusSeeOther {
		t.Fatalf("bob delete own token = %d, want 303 (body %s)", deleteAsBob.Code, deleteAsBob.Body)
	}
	bobListFinal := httptest.NewRecorder()
	bobListFinalReq := httptest.NewRequest(http.MethodGet, "/account/tokens", nil)
	bobListFinalReq.AddCookie(bobSession)
	h.ServeHTTP(bobListFinal, bobListFinalReq)
	if strings.Contains(bobListFinal.Body.String(), "bob-token") {
		t.Fatalf("bob's own revoke did not remove the token: %s", bobListFinal.Body)
	}
}
