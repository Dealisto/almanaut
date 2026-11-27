package web

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestRelationshipAPIPages(t *testing.T) {
	h, db := newTestServerDB(t)
	if err := store.WithTx(db, func(tx *sql.Tx) error {
		rels := store.NewRelationshipRepo(db).WithTx(tx)
		for i := range 123 {
			if _, err := rels.Create(domain.Relationship{FromType: "host", FromID: int64(i + 1), ToType: "network", ToID: 1, Kind: "connected to"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		count int
		first int64
		next  bool
	}{
		{"", 50, 1, true}, {"?page=2", 50, 51, true},
		{"?page=3", 23, 101, false}, {"?per_page=100&page=2", 23, 101, false},
		{"?page=100", 0, 0, false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			rec := getJSONResp(t, h, "/api/relationships"+tc.query)
			var rels []domain.Relationship
			if err := json.Unmarshal(rec.Body.Bytes(), &rels); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusOK || rels == nil || len(rels) != tc.count {
				t.Fatalf("status=%d records=%d want=%d", rec.Code, len(rels), tc.count)
			}
			for i, rel := range rels {
				if rel.ID != tc.first+int64(i) {
					t.Fatalf("record %d: id=%d", i, rel.ID)
				}
			}
			if rec.Header().Get("X-Total-Count") != "123" || rec.Header().Get("X-Page") == "" || rec.Header().Get("X-Per-Page") == "" {
				t.Fatalf("missing page metadata: %v", rec.Header())
			}
			links := rec.Header().Get("Link")
			if !strings.Contains(links, `rel="first"`) || strings.Contains(links, `rel="next"`) != tc.next {
				t.Fatalf("wrong page links: %s", links)
			}
		})
	}
}

func TestRelationshipAPIEmptyAndFailedReads(t *testing.T) {
	h, db := newTestServerDB(t)
	rec := getJSONResp(t, h, "/api/relationships")
	if rec.Code != http.StatusOK || rec.Body.String() != "[]" || rec.Header().Get("X-Total-Count") != "0" {
		t.Fatalf("empty collection: %d %s %v", rec.Code, rec.Body, rec.Header())
	}
	if _, err := db.Exec("DROP TABLE relationships"); err != nil {
		t.Fatal(err)
	}
	rec = getJSONResp(t, h, "/api/relationships")
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("X-Total-Count") != "" {
		t.Fatalf("failed query advertised a page: %d %v", rec.Code, rec.Header())
	}
}

func TestRelationshipAPIDoesNotLoadOffPageRows(t *testing.T) {
	h, db := newTestServerDB(t)
	if _, err := db.Exec(`INSERT INTO relationships (id, from_type, from_id, to_type, to_id, kind)
		VALUES (1, 'host', 1, 'network', 1, 'connected to'),
		       (2, 'host', 'invalid legacy id', 'network', 1, 'connected to')`); err != nil {
		t.Fatal(err)
	}
	rec := getJSONResp(t, h, "/api/relationships?per_page=1")
	if rec.Code != http.StatusOK || rec.Header().Get("X-Total-Count") != "2" {
		t.Fatalf("off-page row broke a valid page: %d %s", rec.Code, rec.Body)
	}
	rec = getJSONResp(t, h, "/api/relationships?per_page=1&page=2")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("invalid selected row was silently omitted: %d %s", rec.Code, rec.Body)
	}
}

func TestRelationshipPaginationOpenAPIContract(t *testing.T) {
	h := newTestServer(t)
	rec := getJSONResp(t, h, "/api/openapi.json")
	var doc struct {
		Paths map[string]struct {
			Get struct {
				Parameters []struct{ Name string }
				Responses  map[string]struct{ Headers map[string]json.RawMessage }
			}
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	op := doc.Paths["/api/relationships"].Get
	params := map[string]bool{}
	for _, p := range op.Parameters {
		params[p.Name] = true
	}
	if !params["page"] || !params["per_page"] {
		t.Fatal("relationship pagination parameters missing from OpenAPI")
	}
	for _, key := range []string{"Link", "X-Total-Count", "X-Page", "X-Per-Page"} {
		if _, ok := op.Responses["200"].Headers[key]; !ok {
			t.Errorf("pagination header %s missing from OpenAPI", key)
		}
	}
	if _, ok := op.Responses["400"]; !ok {
		t.Fatal("invalid pagination response missing from OpenAPI")
	}
}
