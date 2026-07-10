package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestBulkToolbarRendered(t *testing.T) {
	srv, _ := newTestServerDB(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"a"}, "type": {"physical"}})
	body := getBody(t, srv, "/hosts")
	if !containsAll(body, `id="bulkform"`, `name="ids"`, `value="delete"`) {
		t.Errorf("bulk toolbar/checkboxes not rendered:\n%s", body)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestBulkDelete(t *testing.T) {
	srv, db := newTestServerDB(t)
	for _, n := range []string{"a", "b", "c"} {
		if rec := postForm(t, srv, "/hosts", url.Values{"name": {n}, "type": {"physical"}}); rec.Code != 303 {
			t.Fatalf("create %s = %d", n, rec.Code)
		}
	}
	// Delete hosts 1 and 2 in one batch; 3 survives.
	rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {"delete"}, "ids": {"1", "2"}})
	if rec.Code != 303 {
		t.Fatalf("bulk delete = %d", rec.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("hosts remaining = %d, want 1", n)
	}
	// A delete event was recorded per entity.
	if err := db.QueryRow(`SELECT COUNT(*) FROM changelog WHERE action='delete' AND entity_type='host'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("delete changelog events = %d, want 2", n)
	}
}

func TestBulkTagAddAndRemove(t *testing.T) {
	srv, db := newTestServerDB(t)
	for _, n := range []string{"a", "b"} {
		postForm(t, srv, "/hosts", url.Values{"name": {n}, "type": {"physical"}})
	}
	if rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {"tag-add"}, "ids": {"1", "2"}, "tag": {"prod"}}); rec.Code != 303 {
		t.Fatalf("bulk tag-add = %d", rec.Code)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM tags WHERE name='prod'`).Scan(&n)
	if n != 2 {
		t.Fatalf("tags after add = %d, want 2", n)
	}
	// Remove from one host.
	if rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {"tag-remove"}, "ids": {"1"}, "tag": {"prod"}}); rec.Code != 303 {
		t.Fatalf("bulk tag-remove = %d", rec.Code)
	}
	db.QueryRow(`SELECT COUNT(*) FROM tags WHERE name='prod'`).Scan(&n)
	if n != 1 {
		t.Errorf("tags after remove = %d, want 1", n)
	}
}

func TestBulkSetField(t *testing.T) {
	srv, db := newTestServerDB(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"a"}, "type": {"physical"}, "status": {"running"}})
	postForm(t, srv, "/hosts", url.Values{"name": {"b"}, "type": {"physical"}, "status": {"running"}})
	if rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {"set-field"}, "ids": {"1", "2"}, "field": {"status"}, "value": {"decommissioned"}}); rec.Code != 303 {
		t.Fatalf("bulk set-field = %d", rec.Code)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM hosts WHERE status='decommissioned'`).Scan(&n)
	if n != 2 {
		t.Errorf("hosts with new status = %d, want 2", n)
	}
	// The change was recorded as an update per entity.
	db.QueryRow(`SELECT COUNT(*) FROM changelog WHERE action='update' AND entity_type='host'`).Scan(&n)
	if n != 2 {
		t.Errorf("update events = %d, want 2", n)
	}
}

func TestBulkSetFieldRejectsNonStringField(t *testing.T) {
	srv, _ := newTestServerDB(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"a"}, "type": {"physical"}})
	// u_height is an int field; bulk set-field must refuse it.
	rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {"set-field"}, "ids": {"1"}, "field": {"u_height"}, "value": {"5"}})
	if rec.Code != 400 {
		t.Errorf("set int field = %d, want 400", rec.Code)
	}
}

func TestBulkViewerForbidden(t *testing.T) {
	db := rbacDB(t)
	if err := BootstrapAdmin(store.NewUserRepo(db), testLogger(), "admin", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	h := newAuthedTestHandler(t, db)
	admin := adminSession(t, h)
	if code := csrfPost(t, h, admin, "/hosts", "name=nas&type=physical"); code != http.StatusSeeOther {
		t.Fatalf("admin create host = %d", code)
	}
	viewer := seedUserAndLogin(t, h, db, "vic", domain.RoleViewer)

	// A viewer must not be able to run a bulk action.
	if code := csrfPost(t, h, viewer, "/hosts/bulk", "action=delete&ids=1"); code != http.StatusForbidden {
		t.Errorf("viewer bulk delete = %d, want 403", code)
	}
	// The host must survive.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("hosts = %d, want 1 (viewer delete blocked)", n)
	}
}
