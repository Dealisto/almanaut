package web

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestAgentLargeDisksKeepHostPageAvailable(t *testing.T) {
	handler, _, token := agentServer(t, domain.ScopeAgent)
	cookie := loginAs(t, handler, "alice", "password123")
	for _, tc := range []struct {
		bytes int64
		want  string
	}{
		{1 << 40, "1.0 TiB"},
		{1 << 50, "1.0 PiB"},
		{1 << 60, "1.0 EiB"},
		{math.MaxInt64, "8.0 EiB"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			report := baseReport()
			report.Disks = []agentapi.Disk{{Device: "storage", SizeBytes: tc.bytes, UsedBytes: tc.bytes}}
			if rec := postReport(t, handler, token, report); rec.Code != http.StatusOK {
				t.Fatalf("report status = %d: %s", rec.Code, rec.Body)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/hosts/1", nil), cookie))
			if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), tc.want) < 2 {
				t.Fatalf("host page status = %d; missing size/usage %q", rec.Code, tc.want)
			}
		})
	}
}

func TestAgentDuplicateAddressesUseCanonicalIP(t *testing.T) {
	for _, tc := range []struct{ first, second, canonical string }{
		{"2001:0db8:0:0:0:0:0:1", "2001:db8::1", "2001:db8::1"},
		{"2001:db8::1", "2001:0db8:0:0:0:0:0:1", "2001:db8::1"},
		{"::ffff:192.168.1.10", "192.168.1.10", "192.168.1.10"},
		{"192.168.1.10", "::ffff:192.168.1.10", "192.168.1.10"},
	} {
		t.Run(tc.first, func(t *testing.T) {
			first := domain.Host{ID: 1, Name: "agent-host", Type: "physical", IPs: []string{tc.first}}
			agents, hosts := agentSectionDB(t, first, domain.Host{Name: "manual-host", Type: "physical", IPs: []string{tc.second}})
			if err := agents.Upsert(store.AgentBinding{HostID: 1, AgentID: "uuid-1", Fingerprint: "agent-host|aa", LastSeen: nowRFC3339()}); err != nil {
				t.Fatal(err)
			}
			section := agentSectionFor(agents, hosts)(first, entityCatalog{})
			if len(section.Duplicates) != 1 || section.Duplicates[0].ID != 2 || section.Duplicates[0].Reason != "shares "+tc.canonical {
				t.Fatalf("equivalent addresses not shown as duplicates: %+v", section.Duplicates)
			}
		})
	}
}
