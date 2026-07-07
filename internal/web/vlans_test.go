package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestVLANAndNetworkReference(t *testing.T) {
	srv, _ := newTestServerDB(t)

	if rec := postForm(t, srv, "/vlans", url.Values{"name": {"mgmt"}, "vid": {"10"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create vlan = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	// Create a network referencing VLAN 1.
	if rec := postForm(t, srv, "/networks", url.Values{"name": {"lan"}, "cidr": {"10.0.0.0/24"}, "vlan_id": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create network = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	// Network detail shows the resolved VLAN.
	req := httptest.NewRequest(http.MethodGet, "/networks/1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "mgmt") {
		t.Fatalf("network detail should show VLAN mgmt; got %d", rec.Code)
	}
}

func TestVLANRejectsBadVID(t *testing.T) {
	srv, _ := newTestServerDB(t)
	rec := postForm(t, srv, "/vlans", url.Values{"name": {"x"}, "vid": {"9999"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "vid") {
		t.Fatalf("bad vid should re-render form with error; got %d", rec.Code)
	}
}
