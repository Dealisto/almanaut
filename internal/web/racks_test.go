package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRackHierarchyWiring(t *testing.T) {
	srv, _ := newTestServerDB(t)

	// Create a site, a location in it, and a rack in the location.
	if rec := postForm(t, srv, "/sites", url.Values{"name": {"HQ"}, "address": {"1 St"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create site = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	if rec := postForm(t, srv, "/locations", url.Values{"name": {"Server Room"}, "site_id": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create location = %d, want 303", rec.Code)
	}
	if rec := postForm(t, srv, "/racks", url.Values{"name": {"Rack 1"}, "location_id": {"1"}, "u_height": {"42"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create rack = %d, want 303", rec.Code)
	}

	// The site detail lists its location as a child link.
	req := httptest.NewRequest(http.MethodGet, "/sites/1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sites/1 = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Server Room") || !strings.Contains(body, "/locations/1") {
		t.Fatalf("site detail should link its location; body:\n%s", body)
	}
}

func TestRackRejectsBadUHeight(t *testing.T) {
	srv, _ := newTestServerDB(t)
	rec := postForm(t, srv, "/racks", url.Values{"name": {"R"}, "u_height": {"999"}})
	// Invalid entity re-renders the form (200) with the validation error.
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "u_height") {
		t.Fatalf("bad u_height should re-render form with error; got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAssignHostToRack(t *testing.T) {
	srv, _ := newTestServerDB(t)
	// Need a rack (id 1) first.
	if rec := postForm(t, srv, "/racks", url.Values{"name": {"R1"}, "u_height": {"42"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create rack = %d", rec.Code)
	}
	// Create a host assigned to rack 1 at U10, 2U tall.
	rec := postForm(t, srv, "/hosts", url.Values{
		"name": {"srv"}, "type": {"physical"}, "rack_id": {"1"}, "rack_position": {"10"}, "u_height": {"2"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create placed host = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	// Host detail shows its rack.
	req := httptest.NewRequest(http.MethodGet, "/hosts/1", nil)
	hrec := httptest.NewRecorder()
	srv.ServeHTTP(hrec, req)
	if hrec.Code != http.StatusOK || !strings.Contains(hrec.Body.String(), "R1") {
		t.Fatalf("host detail should show rack R1; got %d", hrec.Code)
	}
}
