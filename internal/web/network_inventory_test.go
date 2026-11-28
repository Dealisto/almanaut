package web

import (
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestStructuredNetworkInventoryHTTP(t *testing.T) {
	db := newWebTestDB(t)
	srv := newAuthedTestHandler(t, db)
	editor := seedUserAndLogin(t, srv, db, "network-editor", domain.RoleEditor)
	viewer := seedUserAndLogin(t, srv, db, "network-viewer", domain.RoleViewer)
	hostID, err := store.NewHostRepo(db).Create(domain.Host{Name: "network-host", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"owner": {"host:" + itoa(hostID)}, "name": {"eth<&>0"}, "kind": {"physical"}, "enabled": {"on"}, "mtu": {"1500"}}
	if rec := csrfPostRec(t, srv, viewer, "/interfaces", form.Encode()); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer write: %d", rec.Code)
	}
	if rec := csrfPostRec(t, srv, editor, "/interfaces", form.Encode()); rec.Code != http.StatusSeeOther {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	interfaces, err := store.NewInterfaceRepo(db).List()
	if err != nil || len(interfaces) != 1 {
		t.Fatalf("interfaces: %+v %v", interfaces, err)
	}
	iface := interfaces[0]
	form.Set("port_id", "invalid")
	if rec := csrfPostRec(t, srv, editor, "/interfaces/"+itoa(iface.ID), form.Encode()); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "port_id must be a valid integer") {
		t.Fatalf("invalid reference was accepted: %d %s", rec.Code, rec.Body)
	}
	ip := url.Values{"address": {"192.0.2.5/24"}, "status": {"active"}, "interface_id": {itoa(iface.ID)}}
	if rec := csrfPostRec(t, srv, editor, "/ip-addresses", ip.Encode()); rec.Code != http.StatusSeeOther {
		t.Fatalf("address: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/interfaces", "/interfaces/" + itoa(iface.ID), "/interfaces/" + itoa(iface.ID) + "/edit", "/ip-addresses", "/ip-addresses/1", "/ip-addresses/1/edit", "/api/interfaces", "/api/ip-addresses"} {
		rec := getWith(t, srv, editor, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	if rec := csrfPostRec(t, srv, editor, "/interfaces/"+itoa(iface.ID)+"/delete", ""); rec.Code != http.StatusConflict {
		t.Fatalf("assigned interface deletion should explain conflict: %d", rec.Code)
	}
	rec := getWith(t, srv, viewer, "/interfaces")
	if !strings.Contains(rec.Body.String(), "eth&lt;&amp;&gt;0") {
		t.Fatal("interface label not escaped")
	}
	var changes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM changelog WHERE entity_type IN ('interface','ipaddress')`).Scan(&changes); err != nil || changes != 2 {
		t.Fatalf("atomic audit: %d %v", changes, err)
	}
}
