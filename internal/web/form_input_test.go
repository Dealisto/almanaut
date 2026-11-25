package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestInvalidNumericFormInputPreservesExistingRelationship(t *testing.T) {
	srv, db := newTestServerDB(t)
	vlanID, err := store.NewVLANRepo(db).Create(domain.VLAN{Name: "production", VID: 42})
	if err != nil {
		t.Fatal(err)
	}
	networks := store.NewNetworkRepo(db)
	id, err := networks.Create(domain.Network{Name: "production", CIDR: "192.0.2.0/24", VLANID: vlanID})
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"bad", "1.5", "9223372036854775808"} {
		t.Run(invalid, func(t *testing.T) {
			rec := postForm(t, srv, "/networks/"+itoa(id), url.Values{"name": {"changed"}, "cidr": {"192.0.2.0/24"}, "vlan_id": {invalid}})
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "vlan_id must be a valid integer") {
				t.Fatalf("invalid form status=%d body=%s", rec.Code, rec.Body.String())
			}
			got, err := networks.Get(id)
			if err != nil || got.VLANID != vlanID || got.Name != "production" {
				t.Fatalf("invalid value changed stored relationship: %+v err=%v", got, err)
			}
		})
	}
	// Numeric fields share whitespace handling across forms and CSV.
	rec := postForm(t, srv, "/networks/"+itoa(id), url.Values{"name": {"renamed"}, "cidr": {"192.0.2.0/24"}, "vlan_id": {" " + itoa(vlanID) + " "}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid padded integer status=%d", rec.Code)
	}
	got, err := networks.Get(id)
	if err != nil || got.VLANID != vlanID || got.Name != "renamed" {
		t.Fatalf("valid padded integer changed relationship: %+v err=%v", got, err)
	}
}

func TestInvalidNumericFormInputDoesNotCreateEntity(t *testing.T) {
	srv, db := newTestServerDB(t)
	rec := postForm(t, srv, "/hosts", url.Values{"name": {"invalid"}, "type": {"physical"}, "rack_id": {"not-a-rack"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "rack_id must be a valid integer") {
		t.Fatalf("invalid create status=%d body=%s", rec.Code, rec.Body.String())
	}
	hosts, err := store.NewHostRepo(db).List()
	if err != nil || len(hosts) != 0 {
		t.Fatalf("invalid numeric input created a host: %+v err=%v", hosts, err)
	}
}

func TestCSVInvalidNumericInputAbortsWholeBatch(t *testing.T) {
	for _, field := range []string{"rack_id", "rack_position", "u_height"} {
		t.Run(field, func(t *testing.T) {
			rs, deps := hostResourceForTest(t)
			id, err := rs.createEntity(deps, domain.Host{Name: "original", Type: "physical"}, nil, "seed")
			if err != nil {
				t.Fatal(err)
			}
			csv := "id,name,type," + field + "\n,new,physical,\n" + itoa(id) + ",changed,physical,bad\n"
			created, updated, rowErrs, err := rs.importCSV(deps, strings.NewReader(csv), "tester")
			if err != nil || created != 0 || updated != 0 || len(rowErrs) != 1 || !strings.Contains(rowErrs[0], "row 3: "+field+" must be a valid integer") {
				t.Fatalf("import created=%d updated=%d errors=%v err=%v", created, updated, rowErrs, err)
			}
			hosts, err := rs.repo.List()
			if err != nil || len(hosts) != 1 || hosts[0].Name != "original" {
				t.Fatalf("invalid numeric CSV changed inventory: %+v err=%v", hosts, err)
			}
		})
	}
}
