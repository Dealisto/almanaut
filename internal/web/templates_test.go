package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestListPageHidesNewForViewer(t *testing.T) {
	db := rbacDB(t)
	users := store.NewUserRepo(db)
	if err := BootstrapAdmin(users, testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	h := newAuthedTestHandler(t, db)
	viewer := seedUserAndLogin(t, h, db, "vic", domain.RoleViewer)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/hosts", nil), viewer))
	if strings.Contains(rec.Body.String(), "/hosts/new") {
		t.Error("viewer must not see the New host control")
	}

	admin := adminSession(t, h)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/hosts", nil), admin))
	if !strings.Contains(rec.Body.String(), "/hosts/new") {
		t.Error("admin should see the New host control")
	}
}

func TestCreateUserFormDefaultsToViewer(t *testing.T) {
	db := rbacDB(t)
	users := store.NewUserRepo(db)
	if err := BootstrapAdmin(users, testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	h := newAuthedTestHandler(t, db)
	admin := adminSession(t, h)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/users", nil), admin))
	body := rec.Body.String()
	// Scope the assertion to the create-user form (the per-row edit selectors
	// legitimately pre-select each existing user's current role, e.g. admin).
	_, createForm, ok := strings.Cut(body, "Add user")
	if !ok {
		t.Fatal("users page missing the Add user form")
	}
	// The create-user form must pre-select the least-privileged role, so an admin
	// who submits without touching the dropdown creates a viewer, not an admin.
	if !strings.Contains(createForm, `value="viewer" selected`) {
		t.Error("create-user form must default the role selector to viewer")
	}
	if strings.Contains(createForm, `value="admin" selected`) {
		t.Error("create-user form must not default the role selector to admin")
	}
}

func TestDetailPageHidesMutatorsForViewer(t *testing.T) {
	db := rbacDB(t)
	users := store.NewUserRepo(db)
	if err := BootstrapAdmin(users, testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	h := newAuthedTestHandler(t, db)
	admin := adminSession(t, h)
	// Create a host to view.
	if code := csrfPost(t, h, admin, "/hosts", "name=nas&type=physical"); code != http.StatusSeeOther {
		t.Fatalf("seed host = %d", code)
	}
	viewer := seedUserAndLogin(t, h, db, "vic", domain.RoleViewer)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/hosts/1", nil), viewer))
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer detail = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "/hosts/1/delete") {
		t.Error("viewer must not see the Delete control on the detail page")
	}
}
