package web

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

func portEffectsFixture(t *testing.T, linked bool) (http.Handler, *sql.DB, *recordingDispatcher) {
	t.Helper()
	srv, db, dispatcher := newTestServerWHDB(t)
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "Owner", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	ports := store.NewPortRepo(db)
	for _, name := range []string{"A", "B", "C", "D"} {
		if _, err := ports.Create(domain.Port{Name: name, OwnerType: "host", OwnerID: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if linked {
		if err := store.WithTx(db, func(tx *sql.Tx) error {
			return ports.UpdateTx(tx, domain.Port{ID: 1, Name: "A", OwnerType: "host", OwnerID: 1, PeerPortID: 2})
		}); err != nil {
			t.Fatal(err)
		}
	}
	return srv, db, dispatcher
}

func portJSONRequest(t *testing.T, srv http.Handler, method, path string, item any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	req = req.WithContext(withUser(req.Context(), domain.User{Username: "cabling", Role: domain.RoleAdmin}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestPortMutationsRecordEveryChangedPeer(t *testing.T) {
	for _, tc := range []struct {
		name         string
		linked       bool
		method, path string
		port         domain.Port
		status       int
		want         map[int64]string
	}{
		{"connect", false, "PUT", "/api/ports/1", domain.Port{ID: 1, Name: "A", OwnerType: "host", OwnerID: 1, PeerPortID: 2}, 200, map[int64]string{1: "updated", 2: "updated"}},
		{"replace", true, "PUT", "/api/ports/1", domain.Port{ID: 1, Name: "A", OwnerType: "host", OwnerID: 1, PeerPortID: 3}, 200, map[int64]string{1: "updated", 2: "updated", 3: "updated"}},
		{"disconnect", true, "PUT", "/api/ports/1", domain.Port{ID: 1, Name: "A", OwnerType: "host", OwnerID: 1}, 200, map[int64]string{1: "updated", 2: "updated"}},
		{"create connected", false, "POST", "/api/ports", domain.Port{Name: "E", OwnerType: "host", OwnerID: 1, PeerPortID: 2}, 201, map[int64]string{5: "created", 2: "updated"}},
		{"delete connected", true, "DELETE", "/api/ports/1", domain.Port{}, 204, map[int64]string{1: "deleted", 2: "updated"}},
		{"no-op", true, "PUT", "/api/ports/1", domain.Port{ID: 1, Name: "A", OwnerType: "host", OwnerID: 1, PeerPortID: 2}, 200, map[int64]string{}},
		{"rename only", true, "PUT", "/api/ports/1", domain.Port{ID: 1, Name: "Renamed", OwnerType: "host", OwnerID: 1, PeerPortID: 2}, 200, map[int64]string{1: "updated"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db, dispatcher := portEffectsFixture(t, tc.linked)
			rec := portJSONRequest(t, srv, tc.method, tc.path, tc.port)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
			}
			events := dispatcher.all()
			if len(events) != len(tc.want) {
				t.Fatalf("events=%+v, want %v", events, tc.want)
			}
			seen := map[int64]bool{}
			for _, event := range events {
				if seen[event.ID] || event.Type != "port" || event.Action != tc.want[event.ID] || event.Actor != "cabling" {
					t.Fatalf("unexpected event %+v", event)
				}
				seen[event.ID] = true
				if event.Action != webhook.ActionDeleted {
					var payload domain.Port
					if err := json.Unmarshal(event.Data, &payload); err != nil {
						t.Fatal(err)
					}
					stored, err := store.NewPortRepo(db).Get(event.ID)
					if err != nil || payload.ID != stored.ID || payload.PeerPortID != stored.PeerPortID {
						t.Fatalf("payload=%+v stored=%+v err=%v", payload, stored, err)
					}
				}
				history, err := store.NewChangelogRepo(db).ListForEntity("port", event.ID)
				if err != nil || len(history) != 1 {
					t.Fatalf("history=%+v err=%v", history, err)
				}
				row := history[0]
				if row.Actor != event.Actor || row.CreatedAt != event.Timestamp || !strings.HasPrefix(row.Label, "Owner / ") {
					t.Fatalf("history metadata=%+v event=%+v", row, event)
				}
				if event.ID != tc.port.ID && event.Action == webhook.ActionUpdated && (len(row.Changes) != 1 || row.Changes[0].Field != "peer_port_id") {
					t.Fatalf("indirect diff=%+v", row.Changes)
				}
			}
			all, err := store.NewChangelogRepo(db).ListRecent(100)
			if err != nil || len(all) != len(tc.want) {
				t.Fatalf("extra history=%+v err=%v", all, err)
			}
		})
	}
}

func TestNICChangesRecordDetachedPorts(t *testing.T) {
	for _, action := range []string{"move", "delete", "no-op"} {
		t.Run(action, func(t *testing.T) {
			srv, db, dispatcher := portEffectsFixture(t, false)
			nic := domain.NIC{Name: "Card", HostID: 1, Kind: "onboard"}
			id, err := store.NewNICRepo(db).Create(nic)
			if err != nil {
				t.Fatal(err)
			}
			nic.ID = id
			if _, err := db.Exec(`UPDATE ports SET nic_id=? WHERE id IN (1,2)`, id); err != nil {
				t.Fatal(err)
			}
			method := "PUT"
			if action == "move" {
				nic.HostID = 2
			}
			if action == "delete" {
				method = "DELETE"
			}
			rec := portJSONRequest(t, srv, method, fmt.Sprintf("/api/nics/%d", id), nic)
			if rec.Code != 200 && rec.Code != 204 {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
			}
			want := 3
			if action == "no-op" {
				want = 0
			}
			events := dispatcher.all()
			if len(events) != want {
				t.Fatalf("events=%+v want %d", events, want)
			}
			for _, event := range events {
				if event.Type != "port" {
					continue
				}
				history, err := store.NewChangelogRepo(db).ListForEntity("port", event.ID)
				if err != nil || len(history) != 1 || !reflect.DeepEqual(history[0].Changes, []domain.FieldChange{{Field: "nic_id", Old: "1", New: "0"}}) {
					t.Fatalf("history=%+v err=%v", history, err)
				}
				var payload domain.Port
				if err := json.Unmarshal(event.Data, &payload); err != nil {
					t.Fatal(err)
				}
				if payload.NICID != 0 || event.Action != "updated" {
					t.Fatalf("event=%+v payload=%+v", event, payload)
				}
			}
		})
	}
}

func TestRelatedPortHistoryFailureRollsBackEverything(t *testing.T) {
	for _, action := range []string{"create", "connect", "delete", "NIC move", "NIC delete"} {
		t.Run(action, func(t *testing.T) {
			srv, db, dispatcher := portEffectsFixture(t, true)
			ports := store.NewPortRepo(db)
			method, path := "PUT", "/api/ports/1"
			var item any = domain.Port{ID: 1, Name: "A", OwnerType: "host", OwnerID: 1}
			switch action {
			case "create":
				method, path = "POST", "/api/ports"
				item = domain.Port{Name: "E", OwnerType: "host", OwnerID: 1, PeerPortID: 3}
			case "delete":
				method = "DELETE"
			case "NIC move", "NIC delete":
				if _, err := store.NewNICRepo(db).Create(domain.NIC{Name: "Card", Kind: "onboard", HostID: 1}); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`UPDATE ports SET nic_id=1 WHERE id=2`); err != nil {
					t.Fatal(err)
				}
				path = "/api/nics/1"
				item = domain.NIC{ID: 1, Name: "Card", Kind: "onboard", HostID: 2}
				if action == "NIC delete" {
					method = "DELETE"
				}
			}
			before, err := ports.List()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TRIGGER fail_related_history BEFORE INSERT ON changelog WHEN NEW.entity_type='port' AND NEW.entity_id IN (2,3) AND NEW.action='update' BEGIN SELECT RAISE(ABORT,'history unavailable'); END`); err != nil {
				t.Fatal(err)
			}
			rec := portJSONRequest(t, srv, method, path, item)
			if rec.Code != 500 {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
			}
			after, err := ports.List()
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("ports changed: before=%+v after=%+v err=%v", before, after, err)
			}
			if strings.HasPrefix(action, "NIC") {
				nic, err := store.NewNICRepo(db).Get(1)
				if err != nil || nic.HostID != 1 {
					t.Fatalf("NIC changed: %+v %v", nic, err)
				}
			}
			history, err := store.NewChangelogRepo(db).ListRecent(100)
			if err != nil || len(history) != 0 || len(dispatcher.all()) != 0 {
				t.Fatalf("failed transaction published history=%+v events=%+v err=%v", history, dispatcher.all(), err)
			}
		})
	}
}

func TestBulkDeleteRecordsDisconnectedPeer(t *testing.T) {
	srv, db, dispatcher := portEffectsFixture(t, true)
	rec := postForm(t, srv, "/ports/bulk", url.Values{"action": {"delete"}, "ids": {"1"}})
	if rec.Code != 303 {
		t.Fatalf("status=%d", rec.Code)
	}
	events := dispatcher.all()
	if len(events) != 2 || events[0].Action != "deleted" || events[1].ID != 2 || events[1].Action != "updated" {
		t.Fatalf("events=%+v", events)
	}
	history, err := store.NewChangelogRepo(db).ListForEntity("port", 2)
	if err != nil || len(history) != 1 {
		t.Fatalf("peer history=%+v err=%v", history, err)
	}
}

func TestCSVPortConnectionsRecordPeersWithoutDuplicateNoops(t *testing.T) {
	srv, db, dispatcher := portEffectsFixture(t, false)
	// The second row writes the reciprocal value already set by the first.
	rec := uploadCSV(t, srv, "port", "id,name,owner_type,owner_id,peer_port_id\n1,A,host,1,2\n2,B,host,1,1\n")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	events := dispatcher.all()
	if len(events) != 2 || events[0].ID != 1 || events[1].ID != 2 {
		t.Fatalf("events=%+v, want one change per port", events)
	}
	for _, id := range []int64{1, 2} {
		history, err := store.NewChangelogRepo(db).ListForEntity("port", id)
		if err != nil || len(history) != 1 {
			t.Fatalf("history for %d=%+v err=%v", id, history, err)
		}
	}
}

func TestCSVPortConnectionFailureDiscardsEarlierPeerEvents(t *testing.T) {
	srv, db, dispatcher := portEffectsFixture(t, false)
	before, err := store.NewPortRepo(db).List()
	if err != nil {
		t.Fatal(err)
	}
	// The first row connects A/B; the second tries to steal occupied B for C.
	rec := uploadCSV(t, srv, "port", "id,name,owner_type,owner_id,peer_port_id\n1,A,host,1,2\n3,C,host,1,2\n")
	if !strings.Contains(rec.Body.String(), "already connected to another port") {
		t.Fatalf("expected an occupied-peer conflict, status=%d", rec.Code)
	}
	if strings.Contains(rec.Header().Get("Location"), "csv_imported=1") {
		t.Fatal("conflicting import succeeded")
	}
	after, err := store.NewPortRepo(db).List()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("ports changed: %+v, %v", after, err)
	}
	history, err := store.NewChangelogRepo(db).ListRecent(100)
	if err != nil || len(history) != 0 || len(dispatcher.all()) != 0 {
		t.Fatalf("partial history/events: %+v %+v %v", history, dispatcher.all(), err)
	}
}
