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
		t.Fatalf("ReportedIPs() = %v, want 2 entries", got)
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

// An unknown schema version must be refused outright rather than guessed at.
func TestValidateRejectsUnknownSchemaVersion(t *testing.T) {
	r := sample()
	r.SchemaVersion = SchemaVersion + 1
	if err := r.Validate(); err == nil {
		t.Fatal("Validate() with future schema version = nil, want error")
	}
}
