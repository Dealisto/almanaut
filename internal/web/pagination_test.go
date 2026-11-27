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
)

func TestInventoryPagesAreBoundedAndAdvertised(t *testing.T) {
	h, db := newTestServerDB(t)
	err := store.WithTx(db, func(tx *sql.Tx) error {
		for i := 0; i < 123; i++ {
			if _, err := store.NewHostRepo(db).WithTx(tx).Create(domain.Host{Name: fmt.Sprintf("host-%03d", i), Type: "physical"}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		count int
		first string
		next  bool
	}{{"", 50, "host-000", true}, {"?page=2", 50, "host-050", true}, {"?page=3", 23, "host-100", false}, {"?page=2&per_page=100", 23, "host-100", false}, {"?page=9", 0, "", false}} {
		t.Run(tc.query, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/hosts"+tc.query, nil))
			if rec.Code != 200 {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var rows []domain.Host
			if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != tc.count {
				t.Fatalf("rows=%d, want %d", len(rows), tc.count)
			}
			if tc.count > 0 && rows[0].Name != tc.first {
				t.Fatalf("first=%s want %s", rows[0].Name, tc.first)
			}
			if rec.Header().Get("X-Total-Count") != "123" {
				t.Fatal("missing total count")
			}
			if strings.Contains(rec.Header().Get("Link"), `rel="next"`) != tc.next {
				t.Fatalf("wrong links: %s", rec.Header().Get("Link"))
			}
		})
	}
	body := getBody(t, h, "/hosts?page=2")
	if !strings.Contains(body, "Showing 51–100 of 123 records") || !strings.Contains(body, "Select all rows on this page") {
		t.Fatal("page count or selection scope missing")
	}
}

func TestPaginationRejectsInvalidBounds(t *testing.T) {
	h := newTestServer(t)
	for _, path := range []string{"/hosts", "/api/hosts", "/search", "/api/search", "/api/relationships"} {
		for _, query := range []string{"page=", "per_page=", "page=0", "page=-1", "page=abc", "per_page=0", "per_page=101", "page=9223372036854775807&per_page=100"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", path+"?"+query, nil))
			if rec.Code != 400 {
				t.Errorf("%s?%s status=%d", path, query, rec.Code)
			}
		}
	}
}

// A broken off-page record must never be hydrated by the list handler.
func TestListDoesNotLoadOffPageEntities(t *testing.T) {
	h, db := newTestServerDB(t)
	if _, err := db.Exec(`INSERT INTO hosts(name,type,ips) VALUES ('a-valid','physical','[]'),('z-invalid','physical','invalid json')`); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/hosts?per_page=1", "/api/hosts?per_page=1"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "a-valid") {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestSQLFilteringPreservesUnicodeCustomFieldsAndNumericOrder(t *testing.T) {
	h, db := newTestServerDB(t)
	def, err := store.NewCustomFieldRepo(db).CreateDef(domain.CustomFieldDef{EntityType: "host", Name: "rank", Label: "Rank", Kind: domain.KindText})
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"ÉCOLE 10", "école 2", "école 1x", "different"} {
		id, err := store.NewHostRepo(db).Create(domain.Host{Name: name, Type: "physical"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO custom_field_values(entity_type,entity_id,def_id,value) VALUES ('host',?,?,?)`, id, def, []string{"10", "2", "1x", "0"}[i]); err != nil {
			t.Fatal(err)
		}
		if i < 3 {
			if _, err := db.Exec(`INSERT INTO tags(entity_type,entity_id,name) VALUES ('host',?,'prod')`, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/hosts?q="+url.QueryEscape("École")+"&tag=prod&sort=Rank&per_page=2", nil))
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got []domain.Host
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 2 || got[1].ID != 1 {
		t.Fatalf("filtered order=%+v", got)
	}
	if rec.Header().Get("X-Total-Count") != "3" || !strings.Contains(rec.Header().Get("Link"), "sort=Rank") || !strings.Contains(rec.Header().Get("Link"), "tag=prod") {
		t.Fatalf("pagination lost filters: %v", rec.Header())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/hosts?field=Rank&value=1x", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "école 1x") || strings.Contains(rec.Body.String(), "école 2") {
		t.Fatalf("custom filter=%s", rec.Body.String())
	}
}

func TestSearchPaginationIncludesTagsAndPreservesCatalogOrder(t *testing.T) {
	h, db := newTestServerDB(t)
	for _, name := range []string{"a-match", "b-match"} {
		if _, err := store.NewHostRepo(db).Create(domain.Host{Name: name, Type: "physical"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.NewServiceRepo(db).Create(domain.Service{Name: "tagged service", Kind: "container"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tags(entity_type,entity_id,name) VALUES ('service',1,'MATCH')`); err != nil {
		t.Fatal(err)
	}
	for page, want := range []string{"host", "host", "service"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", fmt.Sprintf("/api/search?q=match&per_page=1&page=%d", page+1), nil))
		var hits []searchEntry
		if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || len(hits) != 1 || hits[0].Type != want || rec.Header().Get("X-Total-Count") != "3" {
			t.Fatalf("page %d: %s headers=%v", page+1, rec.Body.String(), rec.Header())
		}
	}
	body := getBody(t, h, "/search?q=match&per_page=1&page=3")
	if !strings.Contains(body, "tagged service") || !strings.Contains(body, "Showing 3–3 of 3 records") {
		t.Fatal("browser search page missing tagged match or total")
	}
}

func TestEveryInventoryTypeSupportsDatabasePages(t *testing.T) {
	h, _ := newTestServerDB(t)
	for _, path := range []string{"hosts", "services", "networks", "domains", "certificates", "backups", "hardware", "subscriptions", "accounts", "sites", "locations", "racks", "contacts", "vlans", "reservations", "nics", "ports"} {
		for _, prefix := range []string{"/", "/api/"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", prefix+path+"?q=example&sort=Name&per_page=1", nil))
			if rec.Code != 200 {
				t.Errorf("%s%s: %d %s", prefix, path, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestSQLFiltersPreserveFormattedRelationships(t *testing.T) {
	h, db := newTestServerDB(t)
	rack, err := store.NewRackRepo(db).Create(domain.Rack{Name: "Datacenter Étage", UHeight: 42})
	if err != nil {
		t.Fatal(err)
	}
	host, err := store.NewHostRepo(db).Create(domain.Host{Name: "switch", Type: "physical", RackID: rack, RackPosition: 5, UHeight: 2})
	if err != nil {
		t.Fatal(err)
	}
	nic, err := store.NewNICRepo(db).Create(domain.NIC{Name: "eth0", HostID: host, Kind: "onboard"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewPortRepo(db).Create(domain.Port{Name: "1", OwnerType: "host", OwnerID: host, NICID: nic, MgmtOnly: true}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, field, value string }{{"hosts", "Rack", "ÉTAGE"}, {"hosts", "Rack position (U)", "5 (2U)"}, {"nics", "Host", "switch"}, {"ports", "NIC", "eth0"}, {"ports", "Management only", "yes"}, {"ports", "Connected to", "—"}} {
		q := url.Values{"field": {tc.field}, "value": {tc.value}, "per_page": {"1"}}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/"+tc.path+"?"+q.Encode(), nil))
		var rows []json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || len(rows) != 1 {
			t.Errorf("%s %s: %d %s", tc.path, tc.field, rec.Code, rec.Body.String())
		}
	}
}

func TestSavedQueryExcludesCurrentPage(t *testing.T) {
	h, db := newTestServerDB(t)
	rec := postForm(t, h, "/views", url.Values{"entity_type": {"host"}, "name": {"paged"}, "query": {"q=nas&page=9&per_page=25"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save status=%d %s", rec.Code, rec.Body.String())
	}
	views, err := store.NewSavedViewRepo(db).ListForUser(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 {
		t.Fatal("missing view")
	}
	q, err := url.ParseQuery(views[0].Query)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(q, url.Values{"q": {"nas"}, "per_page": {"25"}}) {
		t.Fatalf("saved query=%v", q)
	}
}

func TestTagSuggestionsAreBoundedWithoutRestrictingFilters(t *testing.T) {
	h, db := newTestServerDB(t)
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "tagged", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	err := store.WithTx(db, func(tx *sql.Tx) error {
		for i := 0; i < 123; i++ {
			if _, err := tx.Exec(`INSERT INTO tags(entity_type,entity_id,name) VALUES ('host',1,?)`, fmt.Sprintf("t%03d", i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	body := getBody(t, h, "/hosts?tag=t122")
	for _, want := range []string{`value="t122"`, `href="/hosts/1?return_to=%2Fhosts%3Ftag%3Dt122"`, "First 100 suggestions; enter any tag."} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, `<option value="t100">`) {
		t.Fatal("tag suggestions exceed their bound")
	}
	// An unfiltered API collection does not need to enumerate UI suggestions.
	if _, err := db.Exec(`DROP TABLE tags`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/hosts", nil))
	if rec.Code != 200 {
		t.Fatalf("API depends on tag suggestions: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDatabaseFilteringTreatsQueryAsLiteralData(t *testing.T) {
	h, db := newTestServerDB(t)
	for _, name := range []string{"100%_ready", "100XXready", "O'Reilly"} {
		if _, err := store.NewHostRepo(db).Create(domain.Host{Name: name, Type: "physical"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{"%_", "O'Reilly"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/hosts?q="+url.QueryEscape(query), nil))
		var got []domain.Host
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || len(got) != 1 {
			t.Fatalf("query %q: %d %s", query, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/hosts?field="+url.QueryEscape("Name); DROP TABLE hosts;--")+"&value=ready", nil))
	if rec.Code != 200 || rec.Body.String() != "[]" {
		t.Fatalf("untrusted column escaped whitelist: %d %s", rec.Code, rec.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("inventory changed: count=%d err=%v", count, err)
	}
}

func TestOpenAPIDocumentsPaginationBoundsAndHeaders(t *testing.T) {
	h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/openapi.json", nil))
	var doc struct {
		Paths map[string]struct {
			Get struct {
				Parameters []struct {
					Name   string
					Schema map[string]any
				}
				Responses map[string]struct{ Headers map[string]any }
			}
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/hosts", "/api/ports", "/api/search"} {
		op := doc.Paths[path].Get
		found := false
		for _, p := range op.Parameters {
			if p.Name == "per_page" {
				found = true
				if p.Schema["default"] != float64(50) || p.Schema["maximum"] != float64(100) || p.Schema["minimum"] != float64(1) {
					t.Fatalf("%s page schema=%v", path, p.Schema)
				}
			}
		}
		if !found {
			t.Errorf("%s missing per_page", path)
		}
		for _, header := range []string{"Link", "X-Page", "X-Per-Page", "X-Total-Count"} {
			if _, ok := op.Responses["200"].Headers[header]; !ok {
				t.Errorf("%s missing %s", path, header)
			}
		}
	}
}
