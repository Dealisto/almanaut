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

// A port link made from one end must show up on both port details and on both
// owners' detail pages.
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

	// Both port detail pages show the link, though only the host port's form set it.
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

// /ports/generate bulk-creates "Prefix 1..N" on one owner, skipping names
// that already exist so re-running it never duplicates.
func TestGeneratePortsIsIdempotent(t *testing.T) {
	srv, db := newTestServerDB(t)
	if rec := postForm(t, srv, "/hardware", url.Values{"name": {"switch01"}, "kind": {"switch"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create hardware = %d", rec.Code)
	}
	form := url.Values{"owner": {"hardware:1"}, "prefix": {"Port "}, "count": {"8"}}
	if rec := postForm(t, srv, "/ports/generate", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("generate = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	countPorts := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ports`).Scan(&n); err != nil {
			t.Fatalf("count ports: %v", err)
		}
		return n
	}
	if n := countPorts(); n != 8 {
		t.Fatalf("generated %d ports, want 8", n)
	}
	// Re-running must not duplicate.
	if rec := postForm(t, srv, "/ports/generate", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("second generate = %d, want 303", rec.Code)
	}
	if n := countPorts(); n != 8 {
		t.Fatalf("re-run duplicated ports: %d, want 8", n)
	}
	// The generated names are "Port 1".."Port 8" on the switch.
	if body := getPage(t, srv, "/ports"); !strings.Contains(body, "Port 8") || strings.Contains(body, "Port 9") {
		t.Error("generated ports should end at Port 8")
	}
}

func TestGeneratePortsClampsCount(t *testing.T) {
	srv, db := newTestServerDB(t)
	if rec := postForm(t, srv, "/hardware", url.Values{"name": {"switch01"}, "kind": {"switch"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create hardware = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/ports/generate", url.Values{"owner": {"hardware:1"}, "count": {"0"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("generate = %d, want 303", rec.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ports`).Scan(&n); err != nil {
		t.Fatalf("count ports: %v", err)
	}
	if n != 1 {
		t.Fatalf("count=0 should clamp to 1 port, got %d", n)
	}
}

func TestGeneratePortsRejectsBadOwner(t *testing.T) {
	srv, _ := newTestServerDB(t)
	rec := postForm(t, srv, "/ports/generate", url.Values{"owner": {"service:1"}, "count": {"4"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad owner = %d, want 400", rec.Code)
	}
}

func TestPortRejectsInvalidOwnerType(t *testing.T) {
	srv, _ := newTestServerDB(t)
	rec := postForm(t, srv, "/ports", url.Values{"name": {"eth0"}, "owner": {"service:1"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "owner") {
		t.Fatalf("invalid owner type should re-render form with error; got %d", rec.Code)
	}
}

// Connecting to a port that is already cabled to another one is a user error:
// the form re-renders with the reason, and the API answers 400, not 500.
func TestPortConnectToTakenPeerIsUserError(t *testing.T) {
	srv, _ := newTestServerDB(t)
	for _, f := range []url.Values{
		{"name": {"Port 1"}, "owner": {"hardware:1"}},
		{"name": {"eth0"}, "owner": {"host:1"}, "peer_port_id": {"1"}},
	} {
		if rec := postForm(t, srv, "/ports", f); rec.Code != http.StatusSeeOther {
			t.Fatalf("create port = %d, body=%s", rec.Code, rec.Body.String())
		}
	}
	rec := postForm(t, srv, "/ports", url.Values{"name": {"eth1"}, "owner": {"host:2"}, "peer_port_id": {"1"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "already connected") {
		t.Fatalf("taken peer should re-render the form with the reason; got %d", rec.Code)
	}
}

func TestPortAPIConnectToTakenPeerIs400(t *testing.T) {
	h, _, raw := apiAuthServer(t)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, body := range []string{
		`{"name":"Port 1","owner_type":"hardware","owner_id":1}`,
		`{"name":"eth0","owner_type":"host","owner_id":1,"peer_port_id":1}`,
		`{"name":"eth1","owner_type":"host","owner_id":2}`,
	} {
		if rec := send(http.MethodPost, "/api/ports", body); rec.Code != http.StatusCreated {
			t.Fatalf("POST %s = %d (body %s)", body, rec.Code, rec.Body)
		}
	}
	rec := send(http.MethodPut, "/api/ports/3", `{"name":"eth1","owner_type":"host","owner_id":2,"peer_port_id":1}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "already connected") {
		t.Fatalf("PUT to a taken peer = %d (body %s), want 400", rec.Code, rec.Body)
	}
	// The switch end reads back its link and can be saved unchanged.
	get := send(http.MethodGet, "/api/ports/1", "")
	if !strings.Contains(get.Body.String(), `"peer_port_id":2`) {
		t.Fatalf("switch port should report its peer, got %s", get.Body)
	}
	if rec := send(http.MethodPut, "/api/ports/1", get.Body.String()); rec.Code != http.StatusOK {
		t.Fatalf("PUT unchanged = %d (body %s)", rec.Code, rec.Body)
	}
	if again := send(http.MethodGet, "/api/ports/2", ""); !strings.Contains(again.Body.String(), `"peer_port_id":1`) {
		t.Fatalf("an unchanged save of one end must keep the other, got %s", again.Body)
	}
}

// A CSV row that contradicts an earlier row of the same file is reported as a
// row error, and the whole import is rolled back.
func TestPortCSVConflictIsRowError(t *testing.T) {
	srv, db := newTestServerDB(t)
	doc := "name,owner_type,owner_id,peer_port_id\n" +
		"Port 1,hardware,1,0\n" +
		"eth0,host,1,1\n" +
		"eth0,host,2,1\n"
	rec := uploadCSV(t, srv, "port", doc)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "row 4") || !strings.Contains(rec.Body.String(), "already connected") {
		t.Fatalf("import-csv = %d, want the form with a row 4 error; body=%s", rec.Code, rec.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ports`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a failed import must write nothing, found %d ports", n)
	}
}

func TestGeneratePortsRejectsNonNumericCount(t *testing.T) {
	srv, _ := newTestServerDB(t)
	rec := postForm(t, srv, "/ports/generate", url.Values{"owner": {"hardware:1"}, "count": {"eight"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-numeric count = %d, want 400", rec.Code)
	}
}
