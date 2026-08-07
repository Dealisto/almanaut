package agentapi

import "testing"

func binding(hostID int64, r Report) *Binding {
	return &Binding{HostID: hostID, AgentID: r.AgentID, Fingerprint: r.Fingerprint()}
}

func TestResolveUpdatesKnownAgent(t *testing.T) {
	r := sample()
	got := Resolve(r, binding(7, r), nil)
	if got.Kind != DecideUpdate || got.HostID != 7 {
		t.Fatalf("Resolve = %+v, want update on host 7", got)
	}
}

// A renamed machine, or one that gained a NIC, is still the same machine: only
// hostname AND every MAC changing at once means the state file was cloned.
func TestResolvePartialFingerprintChangeIsStillUpdate(t *testing.T) {
	old := sample()
	b := binding(7, old)

	renamed := sample()
	renamed.Hostname = "nas01-new"
	if got := Resolve(renamed, b, nil); got.Kind != DecideUpdate {
		t.Fatalf("rename = %+v, want update", got)
	}

	reNICed := sample()
	reNICed.Interfaces = []Interface{{Name: "eth0", MAC: "aa:bb:cc:99:99:99"}}
	if got := Resolve(reNICed, b, nil); got.Kind != DecideUpdate {
		t.Fatalf("new NIC = %+v, want update", got)
	}
}

func TestResolveDetectsClone(t *testing.T) {
	b := binding(7, sample())
	clone := sample()
	clone.Hostname = "web02"
	clone.Interfaces = []Interface{{Name: "eth0", MAC: "de:ad:be:ef:00:01"}}
	if got := Resolve(clone, b, nil); got.Kind != DecideConflict {
		t.Fatalf("clone = %+v, want conflict", got)
	}
}

func TestResolveAdoptsByNormalizedName(t *testing.T) {
	r := sample() // Hostname "nas01"
	hosts := []HostRef{{ID: 3, Name: "  NAS01 "}, {ID: 4, Name: "web01"}}
	got := Resolve(r, nil, hosts)
	if got.Kind != DecideAdopt || got.HostID != 3 {
		t.Fatalf("Resolve = %+v, want adopt host 3", got)
	}
}

func TestResolveAdoptsByIPWhenNameDiffers(t *testing.T) {
	r := sample() // reports 10.0.0.5 and 192.168.1.10
	hosts := []HostRef{{ID: 9, Name: "totally-different", IPs: []string{"192.168.1.10"}}}
	got := Resolve(r, nil, hosts)
	if got.Kind != DecideAdopt || got.HostID != 9 {
		t.Fatalf("Resolve = %+v, want adopt host 9", got)
	}
}

func TestResolveCreatesWhenNothingMatches(t *testing.T) {
	got := Resolve(sample(), nil, []HostRef{{ID: 1, Name: "unrelated", IPs: []string{"172.16.0.1"}}})
	if got.Kind != DecideCreate || got.HostID != 0 {
		t.Fatalf("Resolve = %+v, want create", got)
	}
}

// Both binding and report have no interfaces, hostname differs: vacuous-truth case.
// With no MAC evidence on either side, hostname change is the only signal. We
// deliberately err toward DecideConflict (a loud 409) over the risk of silently
// forking the inventory by accepting it as an update.
func TestResolveConflictWhenBothHaveNoMACsButHostnamesChange(t *testing.T) {
	oldReport := Report{
		SchemaVersion: SchemaVersion,
		AgentID:       "3f2b1c9e-0000-4000-8000-000000000001",
		Hostname:      "nas01",
		Interfaces:    []Interface{},
	}
	b := binding(7, oldReport)

	newReport := Report{
		SchemaVersion: SchemaVersion,
		AgentID:       "3f2b1c9e-0000-4000-8000-000000000001",
		Hostname:      "web02",
		Interfaces:    []Interface{},
	}
	got := Resolve(newReport, b, nil)
	if got.Kind != DecideConflict {
		t.Fatalf("no-MAC conflict = %+v, want conflict", got)
	}
}

// Stored binding has no MACs, new report has MACs, hostname differs: another
// vacuous-truth case. The old fingerprint "nas01|" cannot share any MAC with the
// new report, so both hostname AND "no MACs match" signal a clone. We err toward
// the loud DecideConflict rather than risk silent forking.
func TestResolveConflictWhenOldHasNoMACsNewHasMACs(t *testing.T) {
	oldReport := Report{
		SchemaVersion: SchemaVersion,
		AgentID:       "3f2b1c9e-0000-4000-8000-000000000001",
		Hostname:      "nas01",
		Interfaces:    []Interface{},
	}
	b := binding(7, oldReport)

	newReport := Report{
		SchemaVersion: SchemaVersion,
		AgentID:       "3f2b1c9e-0000-4000-8000-000000000001",
		Hostname:      "web02",
		Interfaces:    []Interface{{Name: "eth0", MAC: "de:ad:be:ef:00:01"}},
	}
	got := Resolve(newReport, b, nil)
	if got.Kind != DecideConflict {
		t.Fatalf("old-no-MAC conflict = %+v, want conflict", got)
	}
}
