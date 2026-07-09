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
