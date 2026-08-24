package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// getPage fetches path and returns the response body, failing on non-200.
func getPage(t *testing.T, srv http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, rec.Code)
	}
	return rec.Body.String()
}

// A one-sided port link must show up on both port details and on both owners'
// detail pages.
func TestPortConnectionShowsOnBothSides(t *testing.T) {
	srv, _ := newTestServerDB(t)

	// switch01 (hardware id 1), server1 (host id 1), onboard NIC (id 1).
	if rec := postForm(t, srv, "/hardware", url.Values{"name": {"switch01"}, "kind": {"switch"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create hardware = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"server1"}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create host = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/nics", url.Values{"name": {"onboard"}, "host_id": {"1"}, "kind": {"onboard"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create nic = %d, body=%s", rec.Code, rec.Body.String())
	}
	// Switch port (port id 1), then the host port pointing at it (port id 2).
	if rec := postForm(t, srv, "/ports", url.Values{"name": {"Port 12"}, "owner": {"hardware:1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create switch port = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec := postForm(t, srv, "/ports", url.Values{
		"name": {"eth0"}, "owner": {"host:1"}, "nic_id": {"1"}, "peer_port_id": {"1"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create host port = %d, body=%s", rec.Code, rec.Body.String())
	}

	// Both port detail pages show the link (stored only on the host port).
	if body := getPage(t, srv, "/ports/2"); !strings.Contains(body, "switch01 / Port 12") {
		t.Error("host port detail should name the switch peer")
	}
	if body := getPage(t, srv, "/ports/1"); !strings.Contains(body, "server1 / eth0") {
		t.Error("switch port detail should name the host peer (reverse direction)")
	}
	// Both owners' detail pages list their port with the connection.
	if body := getPage(t, srv, "/hosts/1"); !strings.Contains(body, "eth0") || !strings.Contains(body, "switch01 / Port 12") {
		t.Error("host detail should list eth0 with its connection")
	}
	if body := getPage(t, srv, "/hardware/1"); !strings.Contains(body, "Port 12") || !strings.Contains(body, "server1 / eth0") {
		t.Error("hardware detail should list Port 12 with its connection")
	}
	// The NIC detail lists its attributed port.
	if body := getPage(t, srv, "/nics/1"); !strings.Contains(body, "eth0") {
		t.Error("nic detail should list its port")
	}
}

func TestPortRejectsInvalidOwnerType(t *testing.T) {
	srv, _ := newTestServerDB(t)
	rec := postForm(t, srv, "/ports", url.Values{"name": {"eth0"}, "owner": {"service:1"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "owner") {
		t.Fatalf("invalid owner type should re-render form with error; got %d", rec.Code)
	}
}
