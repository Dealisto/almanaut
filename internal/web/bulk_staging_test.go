package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func assertBatchEventCounts(t *testing.T, db *sql.DB, recorder *recordingDispatcher, want int) {
	t.Helper()
	for _, table := range []string{"changelog", "webhook_outbox"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count=%d, want %d, err=%v", table, count, want, err)
		}
	}
	if events := recorder.all(); len(events) != want {
		t.Fatalf("post-commit events=%+v, want %d", events, want)
	}
}

func TestBulkFieldStagingAtomicityAndFreshRequestBudget(t *testing.T) {
	for _, capacity := range []int{1, 2} {
		t.Run(itoa(int64(capacity)), func(t *testing.T) {
			srv, db, recorder := newOutboxWebServer(t, capacity)
			hosts := store.NewHostRepo(db)
			for _, name := range []string{"first", "second", "third"} {
				if _, err := hosts.Create(domain.Host{Name: name, Type: "physical"}); err != nil {
					t.Fatal(err)
				}
			}
			form := url.Values{"action": {"set-field"}, "ids": {"1", "2"}, "field": {"notes"}, "value": {"changed"}}
			response := postForm(t, srv, "/hosts/bulk", form)
			want := 2
			if capacity == 1 {
				want = 0
				if response.Code != http.StatusServiceUnavailable {
					t.Fatalf("quota response=%d body=%s", response.Code, response.Body)
				}
			} else if response.Code != http.StatusSeeOther {
				t.Fatalf("bulk response=%d body=%s", response.Code, response.Body)
			}
			assertBatchEventCounts(t, db, recorder, want)
			for _, id := range []int64{1, 2} {
				host, err := hosts.Get(id)
				if err != nil || (want == 0 && host.Notes != "") || (want == 2 && host.Notes != "changed") {
					t.Fatalf("host=%+v err=%v", host, err)
				}
			}
			if capacity == 1 {
				return
			}
			// A full queue must not reject unchanged rows or generate duplicate
			// events, and the next actual mutation must see the committed budget.
			if response := postForm(t, srv, "/hosts/bulk", form); response.Code != http.StatusSeeOther {
				t.Fatalf("no-op rejected: %d %s", response.Code, response.Body)
			}
			form["ids"] = []string{"3"}
			if response := postForm(t, srv, "/hosts/bulk", form); response.Code != http.StatusServiceUnavailable {
				t.Fatalf("new request reused stale budget: %d %s", response.Code, response.Body)
			}
			third, err := hosts.Get(3)
			if err != nil || third.Notes != "" {
				t.Fatalf("rejected request changed host=%+v err=%v", third, err)
			}
			assertBatchEventCounts(t, db, recorder, 2)
		})
	}
}

func TestBulkDeleteStagingCountsDisconnectedPeer(t *testing.T) {
	for _, capacity := range []int{2, 3} {
		t.Run(itoa(int64(capacity)), func(t *testing.T) {
			srv, db, recorder := newOutboxWebServer(t, capacity)
			if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "owner", Type: "physical"}); err != nil {
				t.Fatal(err)
			}
			ports := store.NewPortRepo(db)
			for _, name := range []string{"A", "B", "C"} {
				if _, err := ports.Create(domain.Port{Name: name, OwnerType: "host", OwnerID: 1}); err != nil {
					t.Fatal(err)
				}
			}
			if err := ports.Update(domain.Port{ID: 1, Name: "A", OwnerType: "host", OwnerID: 1, PeerPortID: 2}); err != nil {
				t.Fatal(err)
			}
			response := postForm(t, srv, "/ports/bulk", url.Values{"action": {"delete"}, "ids": {"1", "3"}})
			wantEvents, wantPorts, wantPeer := 3, 1, int64(0)
			if capacity == 2 {
				wantEvents, wantPorts, wantPeer = 0, 3, 1
				if response.Code != http.StatusServiceUnavailable {
					t.Fatalf("quota response=%d body=%s", response.Code, response.Body)
				}
			} else if response.Code != http.StatusSeeOther {
				t.Fatalf("delete response=%d body=%s", response.Code, response.Body)
			}
			assertBatchEventCounts(t, db, recorder, wantEvents)
			all, err := ports.List()
			if err != nil || len(all) != wantPorts {
				t.Fatalf("ports=%+v err=%v", all, err)
			}
			peer, err := ports.Get(2)
			if err != nil || peer.PeerPortID != wantPeer {
				t.Fatalf("indirect peer=%+v err=%v", peer, err)
			}
			if wantEvents > 0 {
				events := recorder.all()
				if events[0].ID != 1 || events[0].Action != "deleted" || events[1].ID != 2 || events[1].Action != "updated" || events[2].ID != 3 || events[2].Action != "deleted" {
					t.Fatalf("direct/indirect events=%+v", events)
				}
			}
		})
	}
}

func TestGeneratedPortStagingAtomicityAndFreshRequestBudget(t *testing.T) {
	for _, capacity := range []int{31, 32} {
		t.Run(itoa(int64(capacity)), func(t *testing.T) {
			srv, db, recorder := newOutboxWebServer(t, capacity)
			if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "switch", Type: "physical"}); err != nil {
				t.Fatal(err)
			}
			form := url.Values{"owner": {"host:1"}, "prefix": {"Port "}, "count": {"32"}}
			response := postForm(t, srv, "/ports/generate", form)
			want := 32
			if capacity == 31 {
				want = 0
				if response.Code != http.StatusServiceUnavailable {
					t.Fatalf("quota response=%d body=%s", response.Code, response.Body)
				}
			} else if response.Code != http.StatusSeeOther {
				t.Fatalf("generation response=%d body=%s", response.Code, response.Body)
			}
			assertBatchEventCounts(t, db, recorder, want)
			ports, err := store.NewPortRepo(db).List()
			if err != nil || len(ports) != want {
				t.Fatalf("generated ports=%d want=%d err=%v", len(ports), want, err)
			}
			if capacity == 31 {
				return
			}
			if response := postForm(t, srv, "/ports/generate", form); response.Code != http.StatusSeeOther {
				t.Fatalf("no-op generation rejected: %d %s", response.Code, response.Body)
			}
			form["count"] = []string{"33"}
			if response := postForm(t, srv, "/ports/generate", form); response.Code != http.StatusServiceUnavailable {
				t.Fatalf("new generation ignored committed budget: %d %s", response.Code, response.Body)
			}
			assertBatchEventCounts(t, db, recorder, 32)
			ports, err = store.NewPortRepo(db).List()
			if err != nil || len(ports) != 32 {
				t.Fatalf("rejected generation changed ports=%d err=%v", len(ports), err)
			}
		})
	}
}

func TestBulkNoopsDoNotReadWebhookConfiguration(t *testing.T) {
	srv, db, recorder := newOutboxWebServer(t, 1)
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "owner", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewPortRepo(db).Create(domain.Port{Name: "Port 1", OwnerType: "host", OwnerID: 1}); err != nil {
		t.Fatal(err)
	}
	// Unavailable webhook configuration must not break a transaction that
	// makes no entity changes. This catches eager scope initialization.
	if _, err := db.Exec(`DROP TABLE webhooks`); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct {
		path string
		form url.Values
	}{
		{"/hosts/bulk", url.Values{"action": {"set-field"}, "ids": {"1"}, "field": {"notes"}, "value": {""}}},
		{"/ports/generate", url.Values{"owner": {"host:1"}, "prefix": {"Port "}, "count": {"1"}}},
	} {
		if response := postForm(t, srv, request.path, request.form); response.Code != http.StatusSeeOther {
			t.Fatalf("%s no-op read webhook configuration: %d %s", request.path, response.Code, response.Body)
		}
	}
	assertBatchEventCounts(t, db, recorder, 0)
}
