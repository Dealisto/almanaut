package web

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestBulkTagHistoryRecordsOnlyActualChanges(t *testing.T) {
	srv, db := newTestServerDB(t)
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"host"}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create host = %d", rec.Code)
	}
	for _, action := range []string{"tag-add", "tag-add", "tag-remove", "tag-remove"} {
		rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {action}, "ids": {"1", "1"}, "tag": {" #PrOd "}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%s = %d", action, rec.Code)
		}
	}
	events, err := store.NewChangelogRepo(db).ListForEntity("host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("history events = %d, want create, add and remove", len(events))
	}
	for i, want := range []domain.FieldChange{
		{Field: "tags", Old: "prod"},
		{Field: "tags", New: "prod"},
	} {
		if len(events[i].Changes) != 1 || events[i].Changes[0] != want {
			t.Errorf("event %d changes = %+v, want %+v", i, events[i].Changes, want)
		}
	}
	tags, err := store.NewTagRepo(db).ListForEntity("host", 1)
	if err != nil || len(tags) != 0 {
		t.Fatalf("tags after removal = %+v, err=%v", tags, err)
	}
}

func TestBulkTagNoopPreservesStaleness(t *testing.T) {
	for _, action := range []string{"tag-add", "tag-remove"} {
		t.Run(action, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			if rec := postForm(t, srv, "/hosts", url.Values{"name": {"host"}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
				t.Fatalf("create host = %d", rec.Code)
			}
			if action == "tag-add" {
				if err := store.NewTagRepo(db).Add(domain.Tag{EntityType: "host", EntityID: 1, Name: "prod"}); err != nil {
					t.Fatal(err)
				}
			}
			const old = "2000-01-01T00:00:00Z"
			if _, err := db.Exec(`UPDATE changelog SET created_at = ?`, old); err != nil {
				t.Fatal(err)
			}
			if rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {action}, "ids": {"1"}, "tag": {"#PROD"}}); rec.Code != http.StatusSeeOther {
				t.Fatalf("%s = %d", action, rec.Code)
			}
			activity, err := store.NewChangelogRepo(db).LastActivity()
			if err != nil {
				t.Fatal(err)
			}
			ref := domain.EntityRef{Type: "host", ID: 1}
			if activity[ref] != old {
				t.Fatalf("no-op refreshed last activity to %q", activity[ref])
			}
			if got := domain.StaleRefs([]domain.EntityRef{ref}, activity, time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC), 90); len(got) != 1 {
				t.Fatal("no-op removed the host from stale inventory")
			}
		})
	}
}

func TestBulkTagRejectsEmptyNormalizedName(t *testing.T) {
	srv, _ := newTestServerDB(t)
	for _, action := range []string{"tag-add", "tag-remove"} {
		if rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {action}, "ids": {"1"}, "tag": {" # "}}); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", action, rec.Code)
		}
	}
}

func TestBulkTagNormalizesExactlyOnce(t *testing.T) {
	srv, db := newTestServerDB(t)
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"host"}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create host = %d", rec.Code)
	}
	for _, action := range []string{"tag-add", "tag-add", "tag-remove"} {
		if rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {action}, "ids": {"1"}, "tag": {"##Prod"}}); rec.Code != http.StatusSeeOther {
			t.Fatalf("%s = %d", action, rec.Code)
		}
	}
	events, err := store.NewChangelogRepo(db).ListForEntity("host", 1)
	if err != nil || len(events) != 3 {
		t.Fatalf("events = %+v, err=%v", events, err)
	}
	if events[0].Changes[0].Old != "#prod" || events[1].Changes[0].New != "#prod" {
		t.Fatalf("tag normalization diverged from storage: %+v", events[:2])
	}
}
