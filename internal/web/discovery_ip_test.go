package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNetworkDiscoveryImportMatchesEquivalentIPs(t *testing.T) {
	for _, tc := range []struct {
		name, existing, candidate string
	}{
		{"compressed IPv6", "2001:0DB8:0000:0000:0000:0000:0000:0001", "2001:db8::1"},
		{"expanded IPv6", "2001:db8::1", "2001:0DB8:0000:0000:0000:0000:0000:0001"},
		{"mapped IPv4", "::ffff:192.0.2.1", "192.0.2.1"},
		{"plain IPv4", "192.0.2.1", "::ffff:192.0.2.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServerNet(t, fakeNetworkScanner{}, NetDiscoveryOptions{Enabled: true})
			if rec := postForm(t, srv, "/hosts", url.Values{"name": {"manual"}, "type": {"vm"}, "ips": {tc.existing}}); rec.Code != http.StatusSeeOther {
				t.Fatalf("create status = %d", rec.Code)
			}
			rec := postForm(t, srv, "/discovery/network/import", url.Values{"type": {"physical"}, "host": {tc.candidate + "|duplicate|22"}})
			assertImportCount(t, rec, "added", 0)
			assertImportCount(t, rec, "existing", 1)
			if body := getBody(t, srv, "/hosts"); strings.Contains(body, "duplicate") || !strings.Contains(body, "manual") {
				t.Fatal("equivalent address import changed the existing inventory")
			}
		})
	}
}

func TestNetworkDiscoveryImportDeduplicatesEquivalentIPsInOneBatch(t *testing.T) {
	srv := newTestServerNet(t, fakeNetworkScanner{}, NetDiscoveryOptions{Enabled: true})
	rec := postForm(t, srv, "/discovery/network/import", url.Values{
		"type": {"vm"},
		"host": {"2001:db8::1|first|22", "2001:0DB8:0000:0000:0000:0000:0000:0001|duplicate|80"},
	})
	assertImportCount(t, rec, "added", 1)
	assertImportCount(t, rec, "existing", 1)
	if body := getBody(t, srv, "/hosts"); strings.Contains(body, "duplicate") || !strings.Contains(body, "first") {
		t.Fatal("one batch imported equivalent addresses more than once")
	}
}
