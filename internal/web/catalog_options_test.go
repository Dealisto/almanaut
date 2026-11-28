package web

import (
	"context"
	"database/sql"
	"errors"
	"html"
	"net/http"
	"reflect"
	"regexp"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// legacyCatalogOptions reproduces the former repository-based catalog path.
// Keep this independent of SQL label/order expressions, including composite
// labels and each repository's own ordering.
func legacyCatalogOptions(t testing.TB, db *sql.DB) []entityOption {
	t.Helper()
	var options []entityOption
	options = append(options, legacyRepositoryOptions(t, store.NewHostRepo(db).List, "host", func(v domain.Host) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewServiceRepo(db).List, "service", func(v domain.Service) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewNetworkRepo(db).List, "network", func(v domain.Network) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewDomainRepo(db).List, "domain", func(v domain.Domain) (int64, string) { return v.ID, v.FQDN })...)
	options = append(options, legacyRepositoryOptions(t, store.NewCertificateRepo(db).List, "certificate", func(v domain.Certificate) (int64, string) { return v.ID, v.Subject })...)
	options = append(options, legacyRepositoryOptions(t, store.NewBackupRepo(db).List, "backup", func(v domain.Backup) (int64, string) { return v.ID, v.Source })...)
	options = append(options, legacyRepositoryOptions(t, store.NewHardwareRepo(db).List, "hardware", func(v domain.Hardware) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewSubscriptionRepo(db).List, "subscription", func(v domain.Subscription) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewAccountRepo(db).List, "account", func(v domain.Account) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewSiteRepo(db).List, "site", func(v domain.Site) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewLocationRepo(db).List, "location", func(v domain.Location) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewRackRepo(db).List, "rack", func(v domain.Rack) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewContactRepo(db).List, "contact", func(v domain.Contact) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewVLANRepo(db).List, "vlan", func(v domain.VLAN) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewReservationRepo(db).List, "reservation", func(v domain.Reservation) (int64, string) { return v.ID, v.Name })...)
	options = append(options, legacyRepositoryOptions(t, store.NewInterfaceRepo(db).List, "interface", func(v domain.NetworkInterface) (int64, string) { return v.ID, networkInterfaceLabel(v) })...)
	options = append(options, legacyRepositoryOptions(t, store.NewIPAddressRepo(db).List, "ipaddress", func(v domain.IPAddress) (int64, string) { return v.ID, v.Address })...)
	options = append(options, legacyRepositoryOptions(t, store.NewNICRepo(db).List, "nic", func(v domain.NIC) (int64, string) {
		if v.HostName == "" {
			return v.ID, v.Name
		}
		return v.ID, v.HostName + " / " + v.Name
	})...)
	options = append(options, legacyRepositoryOptions(t, store.NewPortRepo(db).List, "port", func(v domain.Port) (int64, string) {
		if v.OwnerName == "" {
			return v.ID, v.Name
		}
		return v.ID, v.OwnerName + " / " + v.Name
	})...)
	return options
}

func legacyRepositoryOptions[T any](t testing.TB, list func() ([]T, error), typ string, label func(T) (int64, string)) []entityOption {
	t.Helper()
	items, err := list()
	if err != nil {
		t.Fatal(err)
	}
	var options []entityOption
	for _, item := range items {
		id, name := label(item)
		options = append(options, entityOptionOf(typ, id, name))
	}
	return options
}

func TestCatalogOptionsMatchLegacyOrderAndLabels(t *testing.T) {
	srv, db := newTestServerDB(t)
	seedCatalogLabelFixtures(t, db)
	// Distinguish insertion order, case-sensitive lexical order, duplicate sort
	// keys and the non-name order of certificates, VLANs, NICs and ports.
	for _, query := range []string{
		`INSERT INTO hosts(id,name,type) VALUES(10,'z','physical'),(11,'a','physical'),(12,'A','physical'),(13,'a','physical')`,
		`INSERT INTO services(id,name,kind) VALUES(10,'z','container'),(11,'a','container'),(12,'A','container'),(13,'a','container')`,
		`INSERT INTO networks(id,name,cidr) VALUES(10,'z','192.0.2.0/24'),(11,'a','192.0.2.0/24'),(12,'A','192.0.2.0/24'),(13,'a','192.0.2.0/24')`,
		`INSERT INTO domains(id,fqdn) VALUES(10,'z'),(11,'a'),(12,'A'),(13,'a')`,
		`INSERT INTO backups(id,source) VALUES(10,'z'),(11,'a'),(12,'A'),(13,'a')`,
		`INSERT INTO hardware(id,name) VALUES(10,'z'),(11,'a'),(12,'A'),(13,'a')`,
		`INSERT INTO subscriptions(id,name) VALUES(10,'z'),(11,'a'),(12,'A'),(13,'a')`,
		`INSERT INTO accounts(id,name) VALUES(10,'z'),(11,'a'),(12,'A'),(13,'a')`,
		`INSERT INTO sites(id,name) VALUES(10,'z'),(11,'a'),(12,'A'),(13,'a')`,
		`INSERT INTO locations(id,name) VALUES(10,'z'),(11,'a'),(12,'A'),(13,'a')`,
		`INSERT INTO racks(id,name) VALUES(10,'z'),(11,'a'),(12,'A'),(13,'a')`,
		`INSERT INTO contacts(id,name) VALUES(10,'z'),(11,'a'),(12,'A'),(13,'a')`,
		`INSERT INTO ip_reservations(id,name) VALUES(10,'z'),(11,'a'),(12,'A'),(13,'a')`,
		`INSERT INTO certificates(id,subject,expires_on) VALUES(10,'z','2030-01-01'),(11,'a','2020-01-01'),(12,'A','2020-01-01'),(13,'a','2010-01-01')`,
		`INSERT INTO vlans(id,name,vid) VALUES(10,'z',2),(11,'a',10),(12,'A',2),(13,'a',1)`,
		`INSERT INTO nics(id,host_id,name) VALUES(10,1,'z'),(11,10,'a'),(12,1,'a'),(13,1,'a')`,
		`INSERT INTO ports(id,owner_type,owner_id,name) VALUES(10,'host',1,'Port 10'),(11,'hardware',1,'Port 2'),(12,'host',1,'Port 2'),(13,'host',1,'Port 2'),(14,'host',2,'Port 1')`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	want := legacyCatalogOptions(t, db)
	pattern := regexp.MustCompile(`<option value="([a-z_]+:[0-9]+)"[^>]*>([^<]*)</option>`)
	for _, path := range []string{"/relationships", "/impact"} {
		response := getJSONResp(t, srv, path)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body)
		}
		matches := pattern.FindAllStringSubmatch(response.Body.String(), -1)
		copies := 1
		if path == "/relationships" {
			copies = 2
		}
		if len(matches) != len(want)*copies {
			t.Fatalf("%s options=%d want=%d", path, len(matches), len(want)*copies)
		}
		for i, match := range matches {
			expected := want[i%len(want)]
			if match[1] != expected.Value || html.UnescapeString(match[2]) != expected.Label {
				t.Errorf("%s option[%d]=%q/%q want=%+v", path, i, match[1], html.UnescapeString(match[2]), expected)
			}
		}
	}
}

func TestCatalogOptionsIgnoreUnneededDecorations(t *testing.T) {
	srv, db := newTestServerDB(t)
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "server", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE host_types`); err != nil {
		t.Fatal(err)
	}
	// The old options path fails on a decoration that is never displayed.
	if _, err := store.NewHostRepo(db).List(); err == nil {
		t.Fatal("legacy list unexpectedly succeeded")
	}
	for _, path := range []string{"/relationships", "/impact"} {
		if response := getJSONResp(t, srv, path); response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body)
		}
	}
}

func TestCatalogOptionsContextAndErrors(t *testing.T) {
	_, db := newTestServerDB(t)
	cat := entityCatalog{db: db, resources: []mountable{resource[domain.Host]{sing: "host", query: inventoryQuery("hosts", "e.name", "e.name")}}}
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "server", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	if got, err := cat.options(); err != nil || !reflect.DeepEqual(got, []entityOption{entityOptionOf("host", 1, "server")}) {
		t.Fatalf("options=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cat.optionsContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error=%v", err)
	}
	if _, err := db.Exec(`DROP TABLE hosts`); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.options(); err == nil {
		t.Fatal("concealed missing inventory table")
	}
}
