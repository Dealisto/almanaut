package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// unbindFixture seeds one host with an agent bound to it and returns the
// handler, the db and the host id.
func unbindFixture(t *testing.T) (http.Handler, *sql.DB, int64) {
	t.Helper()
	db := rbacDB(t)
	h := newAuthedTestHandler(t, db)
	id, err := store.NewHostRepo(db).Create(domain.Host{Name: "nas01", Type: "physical"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := store.NewAgentRepo(db).Upsert(store.AgentBinding{
		HostID: id, AgentID: "uuid-1", Fingerprint: "f", LastSeen: "2026-08-07T10:00:00Z",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	return h, db, id
}

func unbindPath(id int64) string {
	return "/hosts/" + strconv.FormatInt(id, 10) + "/agent/unbind"
}

func TestUnbindRemovesTheBindingAndRecordsIt(t *testing.T) {
	h, db, id := unbindFixture(t)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)

	rec := csrfPostRec(t, h, admin, unbindPath(id), "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (body %s)", rec.Code, rec.Body)
	}

	agents := store.NewAgentRepo(db)
	if _, err := agents.ByHostID(id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("binding survived unbind: %v", err)
	}
	// Freeing the agent id is the point: it is what lets the next report
	// re-adopt this host instead of creating another record.
	if _, err := agents.ByAgentID("uuid-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("agent id still bound: %v", err)
	}

	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM changelog WHERE entity_type='host' AND entity_id=?`, id,
	).Scan(&n); err != nil {
		t.Fatalf("count changelog: %v", err)
	}
	if n != 1 {
		t.Fatalf("changelog rows = %d, want 1 — unbinding changes who owns a record", n)
	}
}

// Unbinding a host that has no agent is not an error: the operator asked for a
// state that already holds. But it must not fabricate history.
func TestUnbindWithNoBindingRedirectsAndWritesNoHistory(t *testing.T) {
	db := rbacDB(t)
	h := newAuthedTestHandler(t, db)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)
	id, err := store.NewHostRepo(db).Create(domain.Host{Name: "nas01", Type: "physical"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}

	if rec := csrfPostRec(t, h, admin, unbindPath(id), ""); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM changelog WHERE entity_type='host' AND entity_id=?`, id).Scan(&n)
	if n != 0 {
		t.Fatalf("changelog rows = %d, want 0 — nothing was unbound", n)
	}
}

// A binding read that fails for a reason other than "no binding exists" must
// not let the delete proceed: a changelog entry naming no agent would look
// like a record of what happened while actually recording nothing. The
// failure is forced by dropping a column ByHostID selects (see below) rather
// than the whole table, because the two code paths must be distinguishable:
// with the read's error ignored, Unbind's DELETE still succeeds and a hollow
// changelog entry is written; with it checked, the DELETE never runs.
func TestUnbindReadFailureWritesNoChangelogEntry(t *testing.T) {
	h, db, id := unbindFixture(t)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)

	// The SELECT in ByHostID must fail while the DELETE in Unbind still
	// succeeds — dropping the whole table fails both statements the same
	// way the old, unfixed code did, so it could never tell the two code
	// paths apart. Dropping one selected column leaves the table (and the
	// unqualified DELETE) intact while making the SELECT's column list
	// invalid.
	if _, err := db.Exec(`ALTER TABLE host_agents DROP COLUMN last_conflict_hostname`); err != nil {
		t.Fatalf("drop last_conflict_hostname column: %v", err)
	}

	rec := csrfPostRec(t, h, admin, unbindPath(id), "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when the binding read fails (body %s)", rec.Code, rec.Body)
	}

	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM changelog WHERE entity_type='host' AND entity_id=?`, id,
	).Scan(&n); err != nil {
		t.Fatalf("count changelog: %v", err)
	}
	if n != 0 {
		t.Fatalf("changelog rows = %d, want 0 — a failed read must not produce a hollow history entry", n)
	}
}

func TestUnbindRejectsAViewer(t *testing.T) {
	h, db, id := unbindFixture(t)
	viewer := seedUserAndLogin(t, h, db, "viewer", domain.RoleViewer)

	if rec := csrfPostRec(t, h, viewer, unbindPath(id), ""); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if _, err := store.NewAgentRepo(db).ByHostID(id); err != nil {
		t.Fatalf("a viewer's rejected POST removed the binding: %v", err)
	}
}

func TestUnbindRejectsAPostWithoutCSRF(t *testing.T) {
	h, db, id := unbindFixture(t)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodPost, unbindPath(id), nil), admin))
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("a POST with no CSRF token succeeded (status %d)", rec.Code)
	}
	if _, err := store.NewAgentRepo(db).ByHostID(id); err != nil {
		t.Fatalf("the CSRF-less POST removed the binding: %v", err)
	}
}

func TestUnbindOnAMissingHostIs404(t *testing.T) {
	h, db, _ := unbindFixture(t)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)
	if rec := csrfPostRec(t, h, admin, unbindPath(9999), ""); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a host that does not exist", rec.Code)
	}
}
