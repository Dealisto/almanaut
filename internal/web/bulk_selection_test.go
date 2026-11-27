package web

import (
	"net/url"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestBulkSelectionComesOnlyFromPostBody(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		selected    []string
		remaining   int
		code        int
	}{
		{"query cannot extend selection", "ids=2", []string{"1"}, 1, 303},
		{"query cannot select unchecked rows", "ids=2", nil, 2, 303},
		{"invalid query cannot reject valid selection", "ids=invalid", []string{"1"}, 1, 303},
		{"invalid body selection is rejected", "ids=2", []string{"1", "invalid"}, 2, 400},
		{"missing body selection rolls back", "ids=2", []string{"1", "999"}, 2, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db, recorder := newOutboxWebServer(t, 10)
			repo := store.NewHostRepo(db)
			for _, name := range []string{"selected", "unselected"} {
				if _, err := repo.Create(domain.Host{Name: name, Type: "physical"}); err != nil {
					t.Fatal(err)
				}
			}
			rec := postForm(t, srv, "/hosts/bulk?"+tc.query, url.Values{"action": {"delete"}, "ids": tc.selected})
			if rec.Code != tc.code {
				t.Fatalf("bulk status=%d body=%s", rec.Code, rec.Body)
			}
			if host, err := repo.Get(2); err != nil || host.Name != "unselected" {
				t.Fatalf("query mutated an unselected row: host=%+v error=%v", host, err)
			}
			var remaining, events int
			if err := db.QueryRow(`SELECT count(*) FROM hosts`).Scan(&remaining); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM changelog`).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if remaining != tc.remaining || events != 2-tc.remaining {
				t.Fatalf("remaining=%d history=%d; want %d/%d", remaining, events, tc.remaining, 2-tc.remaining)
			}
			assertBatchEventCounts(t, db, recorder, 2-tc.remaining)
		})
	}
}

func TestBulkMutationFieldsComeOnlyFromPostBody(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		body        url.Values
		code        int
		status      string
		events      int
	}{
		{"query action", "action=delete", url.Values{"ids": {"1"}}, 400, "original", 0},
		{"query tag", "tag=injected", url.Values{"ids": {"1"}, "action": {"tag-add"}}, 400, "original", 0},
		{"query field", "field=status", url.Values{"ids": {"1"}, "action": {"set-field"}, "value": {"injected"}}, 400, "original", 0},
		{"query value", "value=injected", url.Values{"ids": {"1"}, "action": {"set-field"}, "field": {"status"}}, 303, "", 1},
		{"body action and value", "action=delete&field=name&value=injected", url.Values{"ids": {"1"}, "action": {"set-field"}, "field": {"status"}, "value": {"submitted"}}, 303, "submitted", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db, recorder := newOutboxWebServer(t, 10)
			repo := store.NewHostRepo(db)
			if _, err := repo.Create(domain.Host{Name: "selected", Type: "physical", Status: "original"}); err != nil {
				t.Fatal(err)
			}
			rec := postForm(t, srv, "/hosts/bulk?"+tc.query, tc.body)
			if rec.Code != tc.code {
				t.Fatalf("status=%d body=%s; want %d", rec.Code, rec.Body, tc.code)
			}
			host, err := repo.Get(1)
			if err != nil || host.Name != "selected" || host.Status != tc.status {
				t.Fatalf("unexpected host=%+v err=%v", host, err)
			}
			if tags, err := store.NewTagRepo(db).List(); err != nil || len(tags) != 0 {
				t.Fatalf("query changed tags=%v err=%v", tags, err)
			}
			assertBatchEventCounts(t, db, recorder, tc.events)
		})
	}
}
