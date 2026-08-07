package agentapi

import "testing"

func sample() Report {
	return Report{
		SchemaVersion: SchemaVersion,
		AgentID:       "3f2b1c9e-0000-4000-8000-000000000001",
		AgentVersion:  "0.1.0",
		Hostname:      "nas01",
		VirtKind:      "physical",
		Interfaces: []Interface{
			{Name: "eth1", MAC: "AA:BB:CC:00:00:02", Addrs: []string{"10.0.0.5"}},
			{Name: "eth0", MAC: "aa:bb:cc:00:00:01", Addrs: []string{"192.168.1.10"}},
		},
	}
}

// Fingerprint must not depend on interface enumeration order, which is not
// stable across reboots — otherwise every reboot would look like a cloned VM.
func TestFingerprintIsOrderIndependentAndLowercased(t *testing.T) {
	a := sample()
	b := sample()
	b.Interfaces[0], b.Interfaces[1] = b.Interfaces[1], b.Interfaces[0]
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatalf("fingerprint order-dependent: %q != %q", a.Fingerprint(), b.Fingerprint())
	}
	want := "nas01|aa:bb:cc:00:00:01,aa:bb:cc:00:00:02"
	if a.Fingerprint() != want {
		t.Fatalf("Fingerprint() = %q, want %q", a.Fingerprint(), want)
	}
}

func TestReportedIPsFlattensInterfaces(t *testing.T) {
	got := sample().ReportedIPs()
	if len(got) != 2 {
		t.Fatalf("ReportedIPs() got len %d, want 2", len(got))
	}
	// Assert actual contents and order
	want := []string{"10.0.0.5", "192.168.1.10"}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ReportedIPs() = %v, want %v", got, want)
	}
}

// ReportedIPs must trim whitespace and skip empty strings
func TestReportedIPsTrimsAndSkipsEmpty(t *testing.T) {
	r := Report{
		Interfaces: []Interface{
			{Name: "eth0", MAC: "aa:bb:cc:00:00:01", Addrs: []string{"  192.168.1.10  ", "", "  "}},
			{Name: "eth1", MAC: "aa:bb:cc:00:00:02", Addrs: []string{"10.0.0.5"}},
		},
	}
	got := r.ReportedIPs()
	if len(got) != 2 {
		t.Fatalf("ReportedIPs() got len %d, want 2", len(got))
	}
	want := []string{"192.168.1.10", "10.0.0.5"}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ReportedIPs() = %v, want %v", got, want)
	}
}

// Fingerprint must skip interfaces with empty or whitespace-only MACs
func TestFingerprintSkipsEmptyMACs(t *testing.T) {
	r := Report{
		Hostname: "testhost",
		Interfaces: []Interface{
			{Name: "eth0", MAC: "aa:bb:cc:00:00:01"},
			{Name: "eth1", MAC: ""},
			{Name: "eth2", MAC: "  "},
			{Name: "eth3", MAC: "AA:BB:CC:00:00:02"},
		},
	}
	got := r.Fingerprint()
	want := "testhost|aa:bb:cc:00:00:01,aa:bb:cc:00:00:02"
	if got != want {
		t.Fatalf("Fingerprint() = %q, want %q", got, want)
	}
}

// Fingerprint must handle zero interfaces gracefully and not panic
func TestFingerprintWithZeroInterfaces(t *testing.T) {
	r := Report{
		Hostname:   "lonely",
		Interfaces: []Interface{},
	}
	got := r.Fingerprint()
	want := "lonely|"
	if got != want {
		t.Fatalf("Fingerprint() = %q, want %q", got, want)
	}
}

func TestValidateRejectsMissingAgentIDAndHostname(t *testing.T) {
	r := sample()
	r.AgentID = ""
	if err := r.Validate(); err == nil {
		t.Fatal("Validate() with empty AgentID = nil, want error")
	}
	r = sample()
	r.Hostname = ""
	if err := r.Validate(); err == nil {
		t.Fatal("Validate() with empty Hostname = nil, want error")
	}
}

// ReportedIPs must strip a CIDR suffix and an IPv6 zone, and drop loopback,
// link-local, and unparseable addresses — the exact form net.Interfaces()
// yields on Linux, which net.ParseIP (and so domain.Host.Validate) rejects
// outright. Order and exact contents both matter here.
func TestReportedIPsSanitizesAndFiltersJunk(t *testing.T) {
	r := Report{
		Interfaces: []Interface{
			{Name: "eth0", Addrs: []string{"192.168.1.5/24", "127.0.0.1", "10.0.0.9"}},
			{Name: "eth1", Addrs: []string{"fe80::1%eth1", "::1", "169.254.1.1", "not-an-ip"}},
		},
	}
	got := r.ReportedIPs()
	want := []string{"192.168.1.5", "10.0.0.9"}
	if len(got) != len(want) {
		t.Fatalf("ReportedIPs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ReportedIPs()[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// An unknown schema version must be refused outright rather than guessed at.
func TestValidateRejectsUnknownSchemaVersion(t *testing.T) {
	r := sample()
	r.SchemaVersion = SchemaVersion + 1
	if err := r.Validate(); err == nil {
		t.Fatal("Validate() with future schema version = nil, want error")
	}
}
