package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestSavedViewCreateShowsInSidebarAndManage(t *testing.T) {
	srv, _ := newTestServerDB(t)
	if rec := postForm(t, srv, "/views", url.Values{
		"entity_type": {"host"}, "name": {"Down hosts"}, "query": {"field=Status&value=down"},
	}); rec.Code != 303 {
		t.Fatalf("create view = %d", rec.Code)
	}

	// The sidebar (rendered on every page) lists it under its type.
	home := getBody(t, srv, "/")
	if !strings.Contains(home, "Down hosts") || !strings.Contains(home, "/hosts?field=Status&amp;value=down") {
		t.Errorf("sidebar missing saved view link:\n%s", home)
	}

	// The management page lists it with rename/delete controls.
	manage := getBody(t, srv, "/views")
	if !strings.Contains(manage, "Down hosts") || !strings.Contains(manage, "/views/1/delete") {
		t.Errorf("manage page missing view:\n%s", manage)
	}
}

func TestSavedViewRenameAndDelete(t *testing.T) {
	srv, db := newTestServerDB(t)
	postForm(t, srv, "/views", url.Values{"entity_type": {"host"}, "name": {"orig"}, "query": {"sort=Name"}})

	if rec := postForm(t, srv, "/views/1/rename", url.Values{"name": {"renamed"}}); rec.Code != 303 {
		t.Fatalf("rename = %d", rec.Code)
	}
	var name string
	db.QueryRow(`SELECT name FROM saved_views WHERE id=1`).Scan(&name)
	if name != "renamed" {
		t.Errorf("name = %q, want renamed", name)
	}

	if rec := postForm(t, srv, "/views/1/delete", url.Values{}); rec.Code != 303 {
		t.Fatalf("delete = %d", rec.Code)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM saved_views`).Scan(&n)
	if n != 0 {
		t.Errorf("view survived delete: %d", n)
	}
}

func TestSavedViewInvalidType(t *testing.T) {
	srv, _ := newTestServerDB(t)
	rec := postForm(t, srv, "/views", url.Values{"entity_type": {"nonsense"}, "name": {"x"}})
	if rec.Code != 400 {
		t.Errorf("invalid type = %d, want 400", rec.Code)
	}
}

// TestSavedViewPrivateToOwner verifies one user cannot see or delete another's
// saved view.
func TestSavedViewPrivateToOwner(t *testing.T) {
	db := rbacDB(t)
	if err := BootstrapAdmin(store.NewUserRepo(db), testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	h := newAuthedTestHandler(t, db)
	alice := seedUserAndLogin(t, h, db, "alice", domain.RoleEditor)
	bob := seedUserAndLogin(t, h, db, "bob", domain.RoleEditor)

	// Alice saves a view.
	if code := csrfPost(t, h, alice, "/views", "entity_type=host&name=alice-view&query=sort=Name"); code != http.StatusSeeOther {
		t.Fatalf("alice create view = %d", code)
	}

	// Bob's management page must not show it.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/views", nil), bob))
	if strings.Contains(rec.Body.String(), "alice-view") {
		t.Error("bob can see alice's saved view")
	}

	// Bob cannot delete it: not found for him, and it survives.
	if code := csrfPost(t, h, bob, "/views/1/delete", ""); code != http.StatusNotFound {
		t.Errorf("bob delete alice view = %d, want 404", code)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM saved_views WHERE user_id != 0`).Scan(&n)
	if n != 1 {
		t.Errorf("alice's view count = %d, want 1 (bob delete blocked)", n)
	}
}
