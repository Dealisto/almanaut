package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReservationShowsOnNetworkIPAM(t *testing.T) {
	srv, _ := newTestServerDB(t)

	// A network (id 1) and a reservation inside it.
	if rec := postForm(t, srv, "/networks", url.Values{"name": {"lan"}, "cidr": {"10.0.0.0/24"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create network = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/reservations", url.Values{
		"name": {"dhcp-pool"}, "network_id": {"1"}, "start_ip": {"10.0.0.100"}, "end_ip": {"10.0.0.150"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create reservation = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	// The network detail's IPAM section names the reservation.
	req := httptest.NewRequest(http.MethodGet, "/networks/1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "dhcp-pool") {
		t.Fatalf("network IPAM should list the reservation; got %d", rec.Code)
	}
}

func TestReservationRejectsBadRange(t *testing.T) {
	srv, _ := newTestServerDB(t)
	rec := postForm(t, srv, "/reservations", url.Values{
		"name": {"bad"}, "start_ip": {"10.0.0.50"}, "end_ip": {"10.0.0.10"},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "start") {
		t.Fatalf("reversed range should re-render form with error; got %d", rec.Code)
	}
}
