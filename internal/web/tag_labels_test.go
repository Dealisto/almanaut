package web

import (
	"database/sql"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestTagDrilldownDoesNotReadUnrelatedInventory(t *testing.T) {
	srv, db := newTestServerDB(t)
	id, err := store.NewHostRepo(db).Create(domain.Host{Name: "tagged host", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewTagRepo(db).Add(domain.Tag{EntityType: "host", EntityID: id, Name: "selected"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE services`); err != nil {
		t.Fatal(err)
	}
	response := getJSONResp(t, srv, "/tags?name=selected")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "host: tagged host") {
		t.Fatalf("unrelated inventory broke tag drilldown: status=%d body=%s", response.Code, response.Body)
	}
	if _, err := db.Exec(`DROP TABLE hosts`); err != nil {
		t.Fatal(err)
	}
	// An empty drilldown must not touch any inventory table either.
	response = getJSONResp(t, srv, "/tags?name=absent")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "No entities carry this tag") {
		t.Fatalf("empty tag read inventory: status=%d body=%s", response.Code, response.Body)
	}
	// A referenced table failure is still an infrastructure error, not a
	// deleted-entity fallback that could conceal database damage.
	if response := getJSONResp(t, srv, "/tags?name=selected"); response.Code != http.StatusInternalServerError {
		t.Fatalf("referenced table error concealed: status=%d body=%s", response.Code, response.Body)
	}
}

func TestTagLabelsMatchCatalogAcrossAllEntityTypes(t *testing.T) {
	srv, db := newTestServerDB(t)
	seedCatalogLabelFixtures(t, db)
	// The independent oracle still uses repository entities and Go labels.
	options := labelMap(legacyCatalogOptions(t, db))
	refs := []domain.EntityRef{{Type: "nic", ID: 2}, {Type: "nic", ID: 3}, {Type: "port", ID: 2}, {Type: "port", ID: 3}}
	for _, typ := range domain.EntityTypes {
		refs = append(refs, domain.EntityRef{Type: typ, ID: 1})
	}
	want := make(map[string]string)
	tags := store.NewTagRepo(db)
	for _, ref := range refs {
		key := ref.Type + ":" + itoa(ref.ID)
		label, ok := options[key]
		if !ok {
			t.Fatalf("fixture missing catalog label for %s", key)
		}
		base := ref.Type + "s"
		if ref.Type == "ipaddress" {
			base = "ip-addresses"
		}
		if ref.Type == "hardware" {
			base = "hardware"
		}
		want["/"+base+"/"+itoa(ref.ID)] = label
		if err := tags.Add(domain.Tag{EntityType: ref.Type, EntityID: ref.ID, Name: "selected"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, ref := range []domain.EntityRef{{Type: "host", ID: 999}, {Type: "legacy", ID: 7}} {
		if err := tags.Add(domain.Tag{EntityType: ref.Type, EntityID: ref.ID, Name: "selected"}); err != nil {
			t.Fatal(err)
		}
		want["/"+ref.Type+"s/"+itoa(ref.ID)] = ref.Type + ":" + itoa(ref.ID) + " (deleted)"
	}
	page := getBody(t, srv, "/tags?name=selected")
	links := regexp.MustCompile(`<li><a href="([^"]+)">([^<]*)</a></li>`).FindAllStringSubmatch(page, -1)
	if len(links) != len(want) {
		t.Fatalf("tag links=%d want=%d", len(links), len(want))
	}
	for _, match := range links {
		path, label := html.UnescapeString(match[1]), html.UnescapeString(match[2])
		if expected, ok := want[path]; !ok || label != expected {
			t.Errorf("tag label %s=%q want=%q", path, label, expected)
		}
	}
}

func seedCatalogLabelFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	snapshot := store.Snapshot{
		Version:       1,
		Interfaces:    []domain.NetworkInterface{{ID: 1, OwnerType: "host", OwnerID: 1, Name: "eth0", Kind: "physical", Enabled: true}},
		IPAddresses:   []domain.IPAddress{{ID: 1, Address: "192.0.2.1/24", InterfaceID: 1, Status: "active"}},
		Hosts:         []domain.Host{{ID: 1, Name: "Owner <&>", Type: "physical"}},
		Services:      []domain.Service{{ID: 1, Name: "Service", Kind: "container"}},
		Networks:      []domain.Network{{ID: 1, Name: "Network", CIDR: "192.0.2.0/24"}},
		Domains:       []domain.Domain{{ID: 1, FQDN: "example.test"}},
		Certificates:  []domain.Certificate{{ID: 1, Subject: "Subject", ExpiresOn: "2027-01-01"}},
		Backups:       []domain.Backup{{ID: 1, Source: "Source"}},
		Hardware:      []domain.Hardware{{ID: 1, Name: "Switch", Kind: "switch"}},
		Subscriptions: []domain.Subscription{{ID: 1, Name: "Subscription"}},
		Accounts:      []domain.Account{{ID: 1, Name: "Account"}},
		Sites:         []domain.Site{{ID: 1, Name: "Site"}},
		Locations:     []domain.Location{{ID: 1, Name: "Location", SiteID: 1}},
		Racks:         []domain.Rack{{ID: 1, Name: "Rack", UHeight: 42}},
		Contacts:      []domain.Contact{{ID: 1, Name: "Contact"}},
		VLANs:         []domain.VLAN{{ID: 1, Name: "VLAN", VID: 20}},
		Reservations:  []domain.Reservation{{ID: 1, Name: "Reservation", NetworkID: 1, StartIP: "192.0.2.10", EndIP: "192.0.2.11"}},
		NICs:          []domain.NIC{{ID: 1, Name: "NIC", HostID: 1, Kind: "onboard"}},
		Ports:         []domain.Port{{ID: 1, Name: "Port", OwnerType: "hardware", OwnerID: 1}},
	}
	if err := store.Import(db, snapshot); err != nil {
		t.Fatal(err)
	}
	// Legacy empty owners and soft references to deleted owners must keep the
	// same composed labels as the repository-backed relationship dropdown.
	for _, query := range []string{
		`INSERT INTO hosts(id,name,type) VALUES(2,'','physical')`,
		`INSERT INTO nics(id,name,host_id) VALUES(2,'empty-owner',2),(3,'orphan',999)`,
		`INSERT INTO ports(id,name,owner_type,owner_id) VALUES(2,'empty-owner','host',2),(3,'orphan','hardware',999)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}
