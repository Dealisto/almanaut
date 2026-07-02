package web

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestJournalAddAndDelete(t *testing.T) {
	srv, db := newTestServerDB(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}})

	rec := postForm(t, srv, "/hosts/1/journal", url.Values{"kind": {"incident"}, "body": {"disk failed"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /hosts/1/journal = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/hosts/1" {
		t.Errorf("redirect Location = %q, want /hosts/1", loc)
	}

	entries, err := store.NewJournalRepo(db).ListForEntity("host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != "incident" || entries[0].Body != "disk failed" {
		t.Fatalf("entry not created: %+v", entries)
	}

	postForm(t, srv, fmt.Sprintf("/journal/%d/delete", entries[0].ID), nil)
	entries, _ = store.NewJournalRepo(db).ListForEntity("host", 1)
	if len(entries) != 0 {
		t.Fatalf("entry not deleted: %+v", entries)
	}
}

func TestJournalAddRejectsInvalidKind(t *testing.T) {
	srv, db := newTestServerDB(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}})

	rec := postForm(t, srv, "/hosts/1/journal", url.Values{"kind": {"bogus"}, "body": {"note"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /hosts/1/journal with invalid kind = %d, want 400", rec.Code)
	}
	entries, err := store.NewJournalRepo(db).ListForEntity("host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid entry should not be created: %+v", entries)
	}
}

func TestDeletingEntityPurgesJournalKeepsChangelog(t *testing.T) {
	srv, db := newTestServerDB(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}})
	postForm(t, srv, "/hosts/1/journal", url.Values{"kind": {"info"}, "body": {"note"}})
	postForm(t, srv, "/hosts/1/delete", nil)

	j, _ := store.NewJournalRepo(db).ListForEntity("host", 1)
	if len(j) != 0 {
		t.Errorf("journal not purged on delete: %+v", j)
	}
	c, _ := store.NewChangelogRepo(db).ListForEntity("host", 1)
	if len(c) == 0 || c[0].Action != domain.ActionDelete {
		t.Errorf("changelog should survive delete: %+v", c)
	}
}
