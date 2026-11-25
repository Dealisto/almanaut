package web

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestSearchMatchesCatalogFields(t *testing.T) {
	h, db := newTestServerDB(t)
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "proxmox", Type: "physical", OS: "Ubuntu 24.04", Notes: "runs the media stack", IPs: []string{"10.0.0.5"}}); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]int{"proxmox": 1, "media": 1, "10.0.0": 1, "UBUNTU": 1, "zzz": 0} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/search?q="+url.QueryEscape(query), nil))
		var hits []searchEntry
		if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || len(hits) != want {
			t.Errorf("query %q status=%d hits=%v", query, rec.Code, hits)
		}
	}
}
