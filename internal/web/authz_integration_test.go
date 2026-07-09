package web

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// End-to-end RBAC enforcement tests: the session role matrix (viewer/editor/
// admin against UI routes) and the API token scope×role intersection.
//
// loginAs (real login POST -> session cookie) already exists in
// tokens_test.go; the helpers below build on it.

func rbacDB(t *testing.T) *sql.DB {
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
	return db
}

func withCookie(r *http.Request, c *http.Cookie) *http.Request { r.AddCookie(c); return r }

// seedUserAndLogin creates a user with role and returns a live session cookie.
func seedUserAndLogin(t *testing.T, h http.Handler, db *sql.DB, username string, role domain.Role) *http.Cookie {
	t.Helper()
	hash, err := hashPassword("password123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	now := nowRFC3339()
	if _, err := store.NewUserRepo(db).Create(domain.User{
		Username: username, Role: role, PasswordHash: hash, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create %s: %v", username, err)
	}
	return loginAs(t, h, username, "password123")
}

func adminSession(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	return loginAs(t, h, "admin", "password123")
}

// csrfPost issues an authenticated POST with a valid CSRF token and returns the
// status code.
func csrfPost(t *testing.T, h http.Handler, cookie *http.Cookie, path, body string) int {
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
	return rec.Code
}

func TestSessionRoleMatrix(t *testing.T) {
	db := rbacDB(t)
	users := store.NewUserRepo(db)
	if err := BootstrapAdmin(users, testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	h := newAuthedTestHandler(t, db)
	viewer := seedUserAndLogin(t, h, db, "vic", domain.RoleViewer)
	editor := seedUserAndLogin(t, h, db, "ed", domain.RoleEditor)

	// Viewer can read a list page.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/hosts", nil), viewer))
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer GET /hosts = %d, want 200", rec.Code)
	}
	// Viewer cannot create (POST create host via form path).
	if code := csrfPost(t, h, viewer, "/hosts", "name=nas&type=physical"); code != http.StatusForbidden {
		t.Fatalf("viewer POST /hosts = %d, want 403", code)
	}
	// Editor can create.
	if code := csrfPost(t, h, editor, "/hosts", "name=nas&type=physical"); code != http.StatusSeeOther {
		t.Fatalf("editor POST /hosts = %d, want 303", code)
	}
	// Viewer cannot reach /users (admin route) — 403, not the page.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/users", nil), viewer))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer GET /users = %d, want 403", rec.Code)
	}
	// Editor cannot reach /users either.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/users", nil), editor))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor GET /users = %d, want 403", rec.Code)
	}
	// Viewer self-service still works: change own password page loads.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/account/password", nil), viewer))
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer GET /account/password = %d, want 200", rec.Code)
	}
}

// mkToken creates a token for a user with the given role+scope and returns the
// raw bearer value.
func mkToken(t *testing.T, db *sql.DB, username string, role domain.Role, scope domain.Scope) string {
	t.Helper()
	hash, _ := hashPassword("password123")
	now := nowRFC3339()
	uid, err := store.NewUserRepo(db).Create(domain.User{Username: username, Role: role, PasswordHash: hash, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create %s: %v", username, err)
	}
	raw, _ := newAPIToken()
	if _, err := store.NewTokenRepo(db).Create(store.APIToken{
		TokenHash: hashToken(raw), UserID: uid, Label: "t", Scope: string(scope), CreatedAt: now,
	}); err != nil {
		t.Fatalf("create token: %v", err)
	}
	return raw
}

func apiPost(t *testing.T, h http.Handler, raw, path, body string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func apiGet(t *testing.T, h http.Handler, raw, path string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestAPITokenScopeRoleIntersection(t *testing.T) {
	db := rbacDB(t)
	users := store.NewUserRepo(db)
	if err := BootstrapAdmin(users, testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	h := newAuthedTestHandler(t, db)

	edRW := mkToken(t, db, "ed", domain.RoleEditor, domain.ScopeReadWrite)
	edRO := mkToken(t, db, "ed_ro", domain.RoleEditor, domain.ScopeReadOnly)
	viRW := mkToken(t, db, "vic", domain.RoleViewer, domain.ScopeReadWrite)

	body := `{"name":"nas","type":"physical"}`
	if code := apiPost(t, h, edRW, "/api/hosts", body); code != http.StatusCreated {
		t.Fatalf("editor+rw POST = %d, want 201", code)
	}
	if code := apiPost(t, h, edRO, "/api/hosts", body); code != http.StatusForbidden {
		t.Fatalf("editor+read-only POST = %d, want 403", code)
	}
	if code := apiPost(t, h, viRW, "/api/hosts", body); code != http.StatusForbidden {
		t.Fatalf("viewer+rw POST = %d, want 403", code)
	}
	// Reads still work for a read-only token.
	if code := apiGet(t, h, edRO, "/api/hosts"); code != http.StatusOK {
		t.Fatalf("editor+read-only GET = %d, want 200", code)
	}
}

func TestAdminCanChangeUserRole(t *testing.T) {
	db := rbacDB(t)
	users := store.NewUserRepo(db)
	if err := BootstrapAdmin(users, testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	h := newAuthedTestHandler(t, db)
	admin := adminSession(t, h)
	// Create a viewer via the admin form, then promote to editor.
	if code := csrfPost(t, h, admin, "/users", "username=bob&password=password123&role=viewer"); code != http.StatusSeeOther {
		t.Fatalf("create bob = %d", code)
	}
	bob, _ := users.GetByUsername("bob")
	if code := csrfPost(t, h, admin, "/users/"+strconv.FormatInt(bob.ID, 10)+"/role", "role=editor"); code != http.StatusSeeOther {
		t.Fatalf("promote bob = %d", code)
	}
	bob, _ = users.GetByUsername("bob")
	if bob.Role != domain.RoleEditor {
		t.Fatalf("bob role = %q, want editor", bob.Role)
	}
}
