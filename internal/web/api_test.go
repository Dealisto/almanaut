package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func getJSONResp(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestAPIListAndGet(t *testing.T) {
	h, db := newTestServerDockerDB(t, fakeScanner{})
	hosts := store.NewHostRepo(db)
	id1, err := hosts.Create(domain.Host{Name: "alpha", Type: "physical"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	if _, err := hosts.Create(domain.Host{Name: "beta", Type: "vm"}); err != nil {
		t.Fatalf("create host: %v", err)
	}

	// List
	rec := getJSONResp(t, h, "/api/hosts")
	if rec.Code != http.StatusOK {
		t.Fatalf("list code = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
	var list []domain.Host
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal list: %v; body=%s", err, rec.Body.String())
	}
	if len(list) != 2 {
		t.Fatalf("want 2 hosts, got %d", len(list))
	}
	if !strings.Contains(rec.Body.String(), `"name"`) {
		t.Errorf("expected snake_case keys, got %s", rec.Body.String())
	}

	// Get one
	rec = getJSONResp(t, h, "/api/hosts/"+strconv.FormatInt(id1, 10))
	if rec.Code != http.StatusOK {
		t.Fatalf("get code = %d", rec.Code)
	}
	var one domain.Host
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatalf("unmarshal one: %v", err)
	}
	if one.Name != "alpha" {
		t.Errorf("got %q", one.Name)
	}

	// Missing id -> 404 JSON
	rec = getJSONResp(t, h, "/api/hosts/99999")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing code = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("404 body not JSON error: %s", rec.Body.String())
	}

	// Bad id -> 400
	rec = getJSONResp(t, h, "/api/hosts/abc")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id code = %d", rec.Code)
	}
}

func TestAPISearch(t *testing.T) {
	h, db := newTestServerDockerDB(t, fakeScanner{})
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "jellyfin-box", Type: "vm"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	rec := getJSONResp(t, h, "/api/search?q=jellyfin")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var hits []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if len(hits) != 1 || hits[0]["label"] != "jellyfin-box" {
		t.Fatalf("want one jellyfin hit, got %v", hits)
	}
	if _, ok := hits[0]["fields"]; ok {
		t.Errorf("internal 'fields' should not be serialized: %v", hits[0])
	}

	// Empty query -> empty array (not null)
	rec = getJSONResp(t, h, "/api/search?q=")
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("empty query body = %q, want []", rec.Body.String())
	}
}

func TestAPIRelationships(t *testing.T) {
	h, db := newTestServerDockerDB(t, fakeScanner{})
	rels := store.NewRelationshipRepo(db)
	if _, err := rels.Create(domain.Relationship{
		FromType: "service", FromID: 1, ToType: "host", ToID: 2, Kind: "runs on",
	}); err != nil {
		t.Fatalf("create rel: %v", err)
	}

	rec := getJSONResp(t, h, "/api/relationships")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var list []domain.Relationship
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list) != 1 || list[0].Kind != "runs on" {
		t.Fatalf("want one relationship, got %v", list)
	}
	if !strings.Contains(rec.Body.String(), `"from_type"`) {
		t.Errorf("expected snake_case keys: %s", rec.Body.String())
	}
}
