package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPortErrorsPreserveUnavailableReferences(t *testing.T) {
	for _, edit := range []bool{false, true} {
		for _, tc := range []struct{ name, field, value string }{
			{"missing NIC", "nic_id", "999"},
			{"missing peer", "peer_port_id", "999"},
			{"occupied peer", "peer_port_id", "3"},
			{"self peer", "peer_port_id", "1"},
		} {
			if !edit && tc.name == "self peer" {
				continue
			}
			t.Run(fmt.Sprintf("%s/edit=%t", tc.name, edit), func(t *testing.T) {
				srv, db := newTestServerDB(t)
				mustPost := func(path string, form url.Values) {
					t.Helper()
					if rec := postForm(t, srv, path, form); rec.Code != http.StatusSeeOther {
						t.Fatalf("POST %s = %d: %s", path, rec.Code, rec.Body)
					}
				}
				mustPost("/hosts", url.Values{"name": {"Host"}, "type": {"physical"}})
				mustPost("/nics", url.Values{"name": {"NIC"}, "host_id": {"1"}, "kind": {"onboard"}})
				for i := 1; i <= 4; i++ {
					form := url.Values{"name": {fmt.Sprintf("Port %d", i)}, "owner": {"host:1"}}
					if i == 1 {
						form.Set("nic_id", "1")
					}
					if i == 2 {
						form.Set("peer_port_id", "1")
					}
					if i == 4 {
						form.Set("peer_port_id", "3")
					}
					mustPost("/ports", form)
				}
				form := url.Values{"name": {"Pending edit"}, "owner": {"host:1"}, "nic_id": {"1"}, "notes": {"Keep this draft"}}
				path := "/ports"
				if edit {
					path = "/ports/1"
					form.Set("peer_port_id", "2")
				}
				form.Set(tc.field, tc.value)
				assertUnchanged := func() {
					t.Helper()
					var count, nic, peer1, peer2 int
					if err := db.QueryRow(`SELECT count(*) FROM ports`).Scan(&count); err != nil {
						t.Fatal(err)
					}
					if err := db.QueryRow(`SELECT nic_id, peer_port_id FROM ports WHERE id=1`).Scan(&nic, &peer1); err != nil {
						t.Fatal(err)
					}
					if err := db.QueryRow(`SELECT peer_port_id FROM ports WHERE id=2`).Scan(&peer2); err != nil {
						t.Fatal(err)
					}
					if count != 4 || nic != 1 || peer1 != 2 || peer2 != 1 {
						t.Fatalf("failed submission changed inventory: count=%d NIC=%d peers=%d/%d", count, nic, peer1, peer2)
					}
				}
				for attempt := 0; attempt < 2; attempt++ {
					rec := postForm(t, srv, path, form)
					if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `role="alert"`) {
						t.Fatalf("invalid reference = %d: %s", rec.Code, rec.Body)
					}
					assertUnchanged()
					selected := selectedFormOption(t, rec.Body.String(), tc.field)
					if selected != tc.value {
						t.Fatalf("%s became %q instead of submitted %q", tc.field, selected, tc.value)
					}
					if !strings.Contains(rec.Body.String(), "(unavailable)") {
						t.Error("missing unavailable option explanation")
					}
					form.Set(tc.field, selected) // Simulate submitting the rendered choice unchanged.
				}
				form.Set(tc.field, "0") // Clearing the pending choice must remain deliberate.
				mustPost(path, form)
				id := 5
				if edit {
					id = 1
				}
				var selected int
				var notes string
				if err := db.QueryRow("SELECT "+tc.field+", notes FROM ports WHERE id=?", id).Scan(&selected, &notes); err != nil {
					t.Fatal(err)
				}
				if selected != 0 || notes != "Keep this draft" {
					t.Fatalf("explicit clear failed: selected=%d notes=%q", selected, notes)
				}
			})
		}
	}
}
