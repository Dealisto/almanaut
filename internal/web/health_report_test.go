package web

import (
	"net/url"
	"strings"
	"testing"
)

// TestHealthReportEmpty asserts an empty inventory renders every rule and the
// "All clear" summary badge.
func TestHealthReportEmpty(t *testing.T) {
	srv, _ := newTestServerDB(t)
	body := getBody(t, srv, "/health-report")
	for _, want := range []string{
		"Health report", "All clear",
		"Hosts without a backup", "Services not linked to a host",
		"Expired certificates", "Certificates linked to nothing",
		"Hardware without a warranty date", "Subscriptions without a renewal date",
		"Orphaned entities",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("health report missing %q:\n%s", want, body)
		}
	}
}

// TestHealthReportFindings drives real creates and asserts the offending
// entities surface under their rule and are counted on the dashboard.
func TestHealthReportFindings(t *testing.T) {
	srv, _ := newTestServerDB(t)

	// A host with no backup relationship, an already-expired certificate, and a
	// piece of hardware with no warranty date — one offender per several rules.
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"lonely-nas"}, "type": {"physical"}, "status": {"running"}}); rec.Code != 303 {
		t.Fatalf("POST /hosts = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/certificates", url.Values{"subject": {"old-cert"}, "expires_on": {"2000-01-01"}}); rec.Code != 303 {
		t.Fatalf("POST /certificates = %d", rec.Code)
	}
	if rec := postForm(t, srv, "/hardware", url.Values{"name": {"mystery-box"}}); rec.Code != 303 {
		t.Fatalf("POST /hardware = %d", rec.Code)
	}

	body := getBody(t, srv, "/health-report")
	for _, want := range []string{"lonely-nas", "old-cert", "mystery-box"} {
		if !strings.Contains(body, want) {
			t.Errorf("health report missing offender %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "All clear") {
		t.Error("expected findings, but report shows All clear")
	}

	// The dashboard summarizes the same findings and links to the report.
	dash := getBody(t, srv, "/")
	if !strings.Contains(dash, "/health-report") || !strings.Contains(dash, "inventory") {
		t.Errorf("dashboard missing inventory health summary:\n%s", dash)
	}
}

// TestHealthReportCertRuleNotDoubleCounted verifies an unlinked certificate
// shows under its dedicated rule but not also under "Orphaned entities".
func TestHealthReportCertRuleNotDoubleCounted(t *testing.T) {
	srv, _ := newTestServerDB(t)
	// Future expiry so it is unlinked but not also "expired"; that way it must
	// surface under exactly one rule.
	if rec := postForm(t, srv, "/certificates", url.Values{"subject": {"floating-cert"}, "expires_on": {"2100-01-01"}}); rec.Code != 303 {
		t.Fatalf("POST /certificates = %d", rec.Code)
	}
	body := getBody(t, srv, "/health-report")
	// It appears once (the cert rule), so exactly one occurrence of the subject.
	if n := strings.Count(body, "floating-cert"); n != 1 {
		t.Errorf("floating-cert appears %d times, want 1 (cert rule only):\n%s", n, body)
	}
}
