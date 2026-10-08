package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func tableCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestChildRowsRequireAnExistingParent: a tag, relationship, journal entry or
// attachment for an entity that does not exist (deleted from another tab, or
// an id not used yet) is refused, not stored to attach to a future entity.
func TestChildRowsRequireAnExistingParent(t *testing.T) {
	srv, db := newTestServerDB(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}})    // host 1
	postForm(t, srv, "/hosts", url.Values{"name": {"router"}, "type": {"physical"}}) // host 2

	if rec := postForm(t, srv, "/tags", url.Values{"entity_type": {"host"}, "entity_id": {"99"}, "tag": {"prod"}}); rec.Code != http.StatusNotFound {
		t.Errorf("tag on missing host = %d, want 404", rec.Code)
	}
	if rec := postForm(t, srv, "/hosts/99/journal", url.Values{"kind": {"info"}, "body": {"note"}}); rec.Code != http.StatusNotFound {
		t.Errorf("journal on missing host = %d, want 404", rec.Code)
	}
	if rec := uploadAttachment(t, srv, "/hosts/99/attachments", "a.txt", []byte("a")); rec.Code != http.StatusNotFound {
		t.Errorf("attachment on missing host = %d, want 404", rec.Code)
	}
	rec := postForm(t, srv, "/relationships", url.Values{"from": {"host:1"}, "to": {"host:99"}, "kind": {"depends on"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "no longer exists") {
		t.Errorf("relationship to missing host = %d, want the form re-rendered with an error", rec.Code)
	}
	for _, table := range []string{"tags", "journal_entries", "attachments", "relationships"} {
		if n := tableCount(t, db, table); n != 0 {
			t.Errorf("%s has %d rows, want 0", table, n)
		}
	}

	// The same writes against the existing host still succeed.
	if rec := postForm(t, srv, "/tags", url.Values{"entity_type": {"host"}, "entity_id": {"1"}, "tag": {"prod"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("tag on existing host = %d, want 303", rec.Code)
	}
	if rec := postForm(t, srv, "/relationships", url.Values{"from": {"host:1"}, "to": {"host:2"}, "kind": {"depends on"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("relationship between existing entities = %d, want 303 (body %s)", rec.Code, rec.Body)
	}
}
