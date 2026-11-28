package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/snmp"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
	"github.com/go-chi/chi/v5"
)

type fakeSNMPScanner struct {
	observation snmp.Observation
	err         error
	calls       int
}

func (f *fakeSNMPScanner) Scan(context.Context) (snmp.Observation, error) {
	f.calls++
	return f.observation, f.err
}

type snmpTestDispatcher struct {
	fail   bool
	events []webhook.Event
}

func (d *snmpTestDispatcher) Dispatch(events ...webhook.Event) {
	d.events = append(d.events, events...)
}
func (d *snmpTestDispatcher) Stage(*sql.Tx, ...webhook.Event) error {
	if d.fail {
		return errors.New("test outbox full")
	}
	return nil
}
func snmpHarness(t *testing.T) (http.Handler, *sql.DB, SNMPSource, *fakeSNMPScanner, *snmpTestDispatcher) {
	t.Helper()
	db := newWebTestDB(t)
	for _, q := range []string{`INSERT INTO hardware(id,name) VALUES(1,'switch <script>')`, `INSERT INTO hosts(id,name,type) VALUES(1,'server','physical')`, `INSERT INTO ports(id,owner_type,owner_id,name) VALUES(1,'hardware',1,'port1'),(2,'host',1,'eth0'),(3,'host',1,'eth1')`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	scanner := &fakeSNMPScanner{observation: snmp.Observation{SystemName: "switch <script>", ObjectID: ".1.2.3", Serial: "serial", LLDPComplete: true, Interfaces: []snmp.Interface{{Index: 7, Name: "port7", Type: 6, MAC: "00:01:02:03:04:05"}}, Neighbors: []snmp.Neighbor{{LocalNumber: 19, LocalID: "port1", LocalSubtype: 5, ChassisID: "server-observed", ChassisSubtype: 7, PortID: "eth0", PortSubtype: 5}}}}
	s := SNMPSource{ID: "switch", Name: "Switch", OwnerType: "hardware", OwnerID: 1, Scanner: scanner}
	manager := snmp.NewManager(db, s.ID, scanner)
	if _, err := manager.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	scanner.calls = 0
	rs := resource[domain.Port]{repo: store.NewPortRepo(db), sing: "port", name: "ports", id: func(p domain.Port) int64 { return p.ID }, setID: func(p *domain.Port, id int64) { p.ID = id }, label: func(p domain.Port) string { return p.Name }}
	cat := entityCatalog{resources: []mountable{rs, resource[domain.Host]{sing: "host", name: "hosts"}, resource[domain.Hardware]{sing: "hardware", name: "hardware"}}}
	dispatcher := &snmpTestDispatcher{}
	deps := handlerDeps{db: db, cat: cat, changelog: store.NewChangelogRepo(db), customFields: store.NewCustomFieldRepo(db), webhooks: dispatcher}
	r := chi.NewRouter()
	mountSNMPDiscovery(r, []SNMPSource{s}, db, cat, rs, deps)
	return r, db, s, scanner, dispatcher
}
func snmpRequest(h http.Handler, method, path string, form url.Values, role domain.Role) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), domain.User{ID: 1, Username: "reviewer", Role: role}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
func TestSNMPReadOnlyPagesAndAuthorization(t *testing.T) {
	h, _, s, scanner, _ := snmpHarness(t)
	token := snmpObservationToken(s, scanner.observation, scanner.observation.Neighbors[0])
	for _, path := range []string{"/discovery/snmp", "/discovery/snmp/switch", "/discovery/snmp/switch/link?neighbor=" + token} {
		w := snmpRequest(h, "GET", path, nil, domain.RoleViewer)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "switch <script>") {
			t.Fatal("unescaped observation")
		}
	}
	if scanner.calls != 0 {
		t.Fatal("GET triggered a device scan")
	}
	for _, path := range []string{"/scan", "/import", "/link/review", "/link/connect"} {
		w := snmpRequest(h, "POST", "/discovery/snmp/switch"+path, nil, domain.RoleViewer)
		if w.Code != 403 {
			t.Fatalf("viewer %s = %d", path, w.Code)
		}
	}
	w := snmpRequest(h, "POST", "/discovery/snmp/switch/scan", nil, domain.RoleEditor)
	if w.Code != 403 {
		t.Fatalf("editor scan=%d", w.Code)
	}
}
func TestSNMPImportCreatesOnlyReviewedNewPortsAtomically(t *testing.T) {
	h, db, s, f, d := snmpHarness(t)
	form := url.Values{"owner_expected": {snmp.Fingerprint([]any{s.OwnerType, s.OwnerID, "switch <script>"})}, "interface": {snmpObservationToken(s, f.observation, f.observation.Interfaces[0])}}
	// A rejected outbox stage must roll back the port and changelog together.
	d.fail = true
	w := snmpRequest(h, "POST", "/discovery/snmp/switch/import", form, domain.RoleEditor)
	if w.Code != 500 {
		t.Fatalf("stage failure=%d %s", w.Code, w.Body)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ports`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("partial creation count=%d err=%v", count, err)
	}
	d.fail = false
	w = snmpRequest(h, "POST", "/discovery/snmp/switch/import", form, domain.RoleEditor)
	if w.Code != 303 {
		t.Fatalf("import=%d %s", w.Code, w.Body)
	}
	var mac string
	if err := db.QueryRow(`SELECT mac FROM ports WHERE name='port7'`).Scan(&mac); err != nil || mac != "00:01:02:03:04:05" {
		t.Fatalf("created mac=%s %v", mac, err)
	}
	if len(d.events) != 1 {
		t.Fatalf("events=%d", len(d.events))
	}
	w = snmpRequest(h, "POST", "/discovery/snmp/switch/import", form, domain.RoleEditor)
	if w.Code != 409 {
		t.Fatalf("duplicate overwrite=%d", w.Code)
	}
	f.observation.Interfaces[0].Name = "renamed"
	w = snmpRequest(h, "POST", "/discovery/snmp/switch/import", form, domain.RoleEditor)
	if w.Code != 409 {
		t.Fatalf("stale observation=%d", w.Code)
	}
}
func TestSNMPReviewedLLDPConnectionPreservesFieldsAndRejectsStalePairs(t *testing.T) {
	h, db, s, f, d := snmpHarness(t)
	if _, err := db.Exec(`INSERT INTO custom_field_values(entity_type,entity_id,def_id,value) VALUES('port',1,123,'keep me')`); err != nil {
		t.Fatal(err)
	}
	a, b, err := snmpReadPair(db, s, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"neighbor": {snmpObservationToken(s, f.observation, f.observation.Neighbors[0])}, "local": {"1"}, "remote": {"2"}, "ports_expected": {snmpPairToken(a, b)}}
	w := snmpRequest(h, "POST", "/discovery/snmp/switch/link/review", form, domain.RoleEditor)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Confirm physical cable") {
		t.Fatalf("review=%d %s", w.Code, w.Body)
	}
	if f.calls != 0 {
		t.Fatal("review made a network request")
	}
	if _, err := db.Exec(`UPDATE ports SET notes='manual change' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	w = snmpRequest(h, "POST", "/discovery/snmp/switch/link/connect", form, domain.RoleEditor)
	if w.Code != 409 {
		t.Fatalf("stale pair=%d", w.Code)
	}
	a, b, err = snmpReadPair(db, s, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	form.Set("ports_expected", snmpPairToken(a, b))
	d.fail = true
	w = snmpRequest(h, "POST", "/discovery/snmp/switch/link/connect", form, domain.RoleEditor)
	if w.Code != 500 {
		t.Fatalf("outbox failure=%d", w.Code)
	}
	var linked int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ports WHERE peer_port_id!=0`).Scan(&linked); err != nil || linked != 0 {
		t.Fatalf("partial cable=%d %v", linked, err)
	}
	d.fail = false
	w = snmpRequest(h, "POST", "/discovery/snmp/switch/link/connect", form, domain.RoleEditor)
	if w.Code != 303 {
		t.Fatalf("connect=%d %s", w.Code, w.Body)
	}
	if len(d.events) != 2 {
		t.Fatalf("missing endpoint events=%d", len(d.events))
	}
	for id, peer := range map[int]int{1: 2, 2: 1} {
		var got int
		if err := db.QueryRow(`SELECT peer_port_id FROM ports WHERE id=?`, id).Scan(&got); err != nil || got != peer {
			t.Fatalf("port %d peer=%d %v", id, got, err)
		}
	}
	var custom string
	if err := db.QueryRow(`SELECT value FROM custom_field_values WHERE entity_id=1`).Scan(&custom); err != nil || custom != "keep me" {
		t.Fatalf("custom field lost: %q %v", custom, err)
	}
	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM changelog WHERE entity_type='port' AND action='update'`).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audit=%d %v", audits, err)
	}
	w = snmpRequest(h, "POST", "/discovery/snmp/switch/link/connect", form, domain.RoleEditor)
	if w.Code != 409 {
		t.Fatalf("occupied endpoints=%d", w.Code)
	}
}
func TestSNMPConnectionRequiresFreshCompleteNeighborAndExplicitLocalOwner(t *testing.T) {
	h, db, s, f, _ := snmpHarness(t)
	a, b, err := snmpReadPair(db, s, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"neighbor": {snmpObservationToken(s, f.observation, f.observation.Neighbors[0])}, "local": {strconv.FormatInt(a.ID, 10)}, "remote": {strconv.FormatInt(b.ID, 10)}, "ports_expected": {snmpPairToken(a, b)}}
	f.observation.LLDPComplete = false
	w := snmpRequest(h, "POST", "/discovery/snmp/switch/link/connect", form, domain.RoleEditor)
	if w.Code != 409 {
		t.Fatalf("incomplete LLDP=%d", w.Code)
	}
	f.observation.LLDPComplete = true
	f.observation.Neighbors[0].PortID = "different"
	w = snmpRequest(h, "POST", "/discovery/snmp/switch/link/connect", form, domain.RoleEditor)
	if w.Code != 409 {
		t.Fatalf("changed neighbor=%d", w.Code)
	}
	if _, _, err := snmpReadPair(db, s, "2", "3"); !errors.Is(err, errSNMPReview) {
		t.Fatalf("accepted wrong owner %v", err)
	}
}

func TestSNMPReviewTokenIncludesConfiguredEndpointFingerprint(t *testing.T) {
	_, _, source, scanner, _ := snmpHarness(t)
	source.Fingerprint = "endpoint-a"
	token := snmpObservationToken(source, scanner.observation, scanner.observation.Neighbors[0])
	source.Fingerprint = "endpoint-b"
	if _, ok := snmpFindNeighbor(source, scanner.observation, token); ok {
		t.Fatal("review from different configured target was accepted")
	}
}
