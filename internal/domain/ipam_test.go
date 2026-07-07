package domain

import "testing"

func TestBuildIPAMOccupancy(t *testing.T) {
	networks := []Network{{ID: 1, Name: "lan", CIDR: "192.168.1.0/24", Gateway: "192.168.1.1"}}
	hosts := []Host{
		{ID: 10, Name: "nas", IPs: []string{"192.168.1.5"}},
		{ID: 11, Name: "pi", IPs: []string{"192.168.1.10", "10.0.0.2"}},
	}
	report := BuildIPAM(networks, hosts, nil)
	if len(report.Networks) != 1 {
		t.Fatalf("Networks = %d, want 1", len(report.Networks))
	}
	u := report.Networks[0]
	if u.TotalUsable != 254 {
		t.Errorf("TotalUsable = %d, want 254", u.TotalUsable)
	}
	if u.UsedCount != 2 {
		t.Errorf("UsedCount = %d, want 2", u.UsedCount)
	}
	if u.FreeCount != 252 {
		t.Errorf("FreeCount = %d, want 252", u.FreeCount)
	}
	if len(u.Used) != 2 || u.Used[0].IP != "192.168.1.5" || u.Used[1].IP != "192.168.1.10" {
		t.Errorf("Used = %+v, want sorted [.5 .10]", u.Used)
	}
	if u.Used[0].HostName != "nas" {
		t.Errorf("Used[0].HostName = %q, want nas", u.Used[0].HostName)
	}
	if len(report.Unassigned) != 1 || report.Unassigned[0].IP != "10.0.0.2" {
		t.Errorf("Unassigned = %+v, want [10.0.0.2]", report.Unassigned)
	}
}

func TestBuildIPAMLongestPrefixWins(t *testing.T) {
	networks := []Network{
		{ID: 1, Name: "big", CIDR: "10.0.0.0/8"},
		{ID: 2, Name: "small", CIDR: "10.1.2.0/24"},
	}
	hosts := []Host{{ID: 1, Name: "h", IPs: []string{"10.1.2.3"}}}
	report := BuildIPAM(networks, hosts, nil)
	for _, u := range report.Networks {
		switch u.Network.ID {
		case 1:
			if u.UsedCount != 0 {
				t.Errorf("big net UsedCount = %d, want 0", u.UsedCount)
			}
		case 2:
			if u.UsedCount != 1 {
				t.Errorf("small net UsedCount = %d, want 1", u.UsedCount)
			}
		}
	}
	if len(report.Unassigned) != 0 {
		t.Errorf("Unassigned = %+v, want none", report.Unassigned)
	}
}

func TestBuildIPAMConflicts(t *testing.T) {
	networks := []Network{{ID: 1, CIDR: "192.168.1.0/24"}}
	hosts := []Host{
		{ID: 1, Name: "a", IPs: []string{"192.168.1.5"}},
		{ID: 2, Name: "b", IPs: []string{"192.168.1.5"}},
	}
	u := BuildIPAM(networks, hosts, nil).Networks[0]
	if u.UsedCount != 1 {
		t.Errorf("UsedCount = %d, want 1 (distinct IPs)", u.UsedCount)
	}
	if len(u.Conflicts) != 1 || len(u.Conflicts[0]) != 2 {
		t.Fatalf("Conflicts = %+v, want one group of 2", u.Conflicts)
	}
}

func TestBuildIPAMNextFree(t *testing.T) {
	networks := []Network{{ID: 1, CIDR: "192.168.1.0/30"}} // usable .1 .2
	hosts := []Host{{ID: 1, Name: "a", IPs: []string{"192.168.1.1"}}}
	u := BuildIPAM(networks, hosts, nil).Networks[0]
	if u.TotalUsable != 2 {
		t.Errorf("TotalUsable = %d, want 2", u.TotalUsable)
	}
	if u.NextFree != "192.168.1.2" {
		t.Errorf("NextFree = %q, want 192.168.1.2", u.NextFree)
	}
}

func TestBuildIPAMNextFreeFull(t *testing.T) {
	networks := []Network{{ID: 1, CIDR: "192.168.1.0/30"}}
	hosts := []Host{
		{ID: 1, IPs: []string{"192.168.1.1"}},
		{ID: 2, IPs: []string{"192.168.1.2"}},
	}
	u := BuildIPAM(networks, hosts, nil).Networks[0]
	if u.FreeCount != 0 {
		t.Errorf("FreeCount = %d, want 0", u.FreeCount)
	}
	if u.NextFree != "" {
		t.Errorf("NextFree = %q, want empty (full)", u.NextFree)
	}
}

func TestBuildIPAMIPv6Unbounded(t *testing.T) {
	networks := []Network{{ID: 1, CIDR: "2001:db8::/64"}}
	hosts := []Host{{ID: 1, Name: "h", IPs: []string{"2001:db8::1"}}}
	u := BuildIPAM(networks, hosts, nil).Networks[0]
	if !u.Unbounded {
		t.Errorf("Unbounded = false, want true for /64")
	}
	if u.UsedCount != 1 {
		t.Errorf("UsedCount = %d, want 1", u.UsedCount)
	}
	if u.NextFree != "" {
		t.Errorf("NextFree = %q, want empty for unbounded subnet", u.NextFree)
	}
}

func TestBuildIPAMInvalidCIDR(t *testing.T) {
	// Validate() rejects this on write, but BuildIPAM must not panic if it occurs.
	networks := []Network{{ID: 1, CIDR: "not-a-cidr"}}
	hosts := []Host{{ID: 1, Name: "h", IPs: []string{"192.168.1.5"}}}
	report := BuildIPAM(networks, hosts, nil)
	if len(report.Networks) != 1 || report.Networks[0].UsedCount != 0 {
		t.Errorf("invalid-CIDR network should have zero usage")
	}
	if len(report.Unassigned) != 1 {
		t.Errorf("IP matching no valid network should be unassigned")
	}
}

func TestBuildIPAMSlash31(t *testing.T) {
	// /31 point-to-point: both addresses are usable (no network/broadcast).
	networks := []Network{{ID: 1, CIDR: "192.168.1.0/31"}}
	hosts := []Host{{ID: 1, Name: "a", IPs: []string{"192.168.1.0"}}}
	u := BuildIPAM(networks, hosts, nil).Networks[0]
	if u.TotalUsable != 2 {
		t.Errorf("TotalUsable = %d, want 2", u.TotalUsable)
	}
	if u.NextFree != "192.168.1.1" {
		t.Errorf("NextFree = %q, want 192.168.1.1", u.NextFree)
	}
}

func TestBuildIPAMSlash32(t *testing.T) {
	// /32 host route: exactly one usable address.
	networks := []Network{{ID: 1, CIDR: "192.168.1.5/32"}}
	u := BuildIPAM(networks, nil, nil).Networks[0]
	if u.TotalUsable != 1 {
		t.Errorf("TotalUsable = %d, want 1", u.TotalUsable)
	}
	if u.FreeCount != 1 {
		t.Errorf("FreeCount = %d, want 1", u.FreeCount)
	}
	if u.NextFree != "192.168.1.5" {
		t.Errorf("NextFree = %q, want 192.168.1.5", u.NextFree)
	}
}

func TestBuildIPAMLargeSubnetCountedNotEnumerated(t *testing.T) {
	// A subnet too large to scan still reports its true size, but suppresses
	// NextFree (the contract: always report size, suggest next-free only when
	// the subnet is small enough to enumerate). It is not marked Unbounded.
	networks := []Network{{ID: 1, CIDR: "10.0.0.0/8"}}
	hosts := []Host{{ID: 1, Name: "a", IPs: []string{"10.1.1.1"}}}
	u := BuildIPAM(networks, hosts, nil).Networks[0]
	if u.Unbounded {
		t.Errorf("Unbounded = true, want false for IPv4 /8")
	}
	if u.TotalUsable != (1<<24)-2 {
		t.Errorf("TotalUsable = %d, want %d", u.TotalUsable, (1<<24)-2)
	}
	if u.UsedCount != 1 {
		t.Errorf("UsedCount = %d, want 1", u.UsedCount)
	}
	if u.NextFree != "" {
		t.Errorf("NextFree = %q, want empty (subnet too large to enumerate)", u.NextFree)
	}
}

func TestBuildNetworkUsageMatchesBuildIPAM(t *testing.T) {
	// BuildNetworkUsage must produce, for the target network, exactly what the
	// corresponding entry of BuildIPAM produces — including the longest-prefix
	// rule: 10.1.2.3 belongs to the more-specific /24, not the /8.
	networks := []Network{
		{ID: 1, Name: "big", CIDR: "10.0.0.0/8", Gateway: "10.0.0.1"},
		{ID: 2, Name: "small", CIDR: "10.1.2.0/24", Gateway: "10.1.2.1"},
		{ID: 3, Name: "lan", CIDR: "192.168.1.0/24"},
	}
	hosts := []Host{
		{ID: 10, Name: "a", IPs: []string{"10.1.2.3"}}, // most-specific: small
		{ID: 11, Name: "b", IPs: []string{"10.9.9.9"}}, // only in big
		{ID: 12, Name: "c", IPs: []string{"192.168.1.5"}},
	}
	report := BuildIPAM(networks, hosts, nil)
	for _, want := range report.Networks {
		got, ok := BuildNetworkUsage(want.Network.ID, networks, hosts, nil)
		if !ok {
			t.Fatalf("BuildNetworkUsage(%d) ok=false, want true", want.Network.ID)
		}
		if got.UsedCount != want.UsedCount || got.TotalUsable != want.TotalUsable ||
			got.FreeCount != want.FreeCount || got.NextFree != want.NextFree ||
			got.Unbounded != want.Unbounded {
			t.Errorf("network %d: BuildNetworkUsage = %+v, want %+v", want.Network.ID, got, want)
		}
		if len(got.Used) != len(want.Used) {
			t.Errorf("network %d: Used len = %d, want %d", want.Network.ID, len(got.Used), len(want.Used))
		}
	}
}

func TestBuildNetworkUsageUnknownTarget(t *testing.T) {
	networks := []Network{{ID: 1, CIDR: "192.168.1.0/24"}}
	if _, ok := BuildNetworkUsage(99, networks, nil, nil); ok {
		t.Error("BuildNetworkUsage for unknown id returned ok=true, want false")
	}
}

func TestBuildIPAMNextFreeSkipsGateway(t *testing.T) {
	// The gateway is reserved: NextFree must not suggest it.
	networks := []Network{{ID: 1, CIDR: "192.168.1.0/24", Gateway: "192.168.1.1"}}
	hosts := []Host{{ID: 1, Name: "a", IPs: []string{"192.168.1.5"}}}
	u := BuildIPAM(networks, hosts, nil).Networks[0]
	if u.NextFree != "192.168.1.2" {
		t.Errorf("NextFree = %q, want 192.168.1.2 (gateway .1 skipped)", u.NextFree)
	}
}

// TestBuildIPAMNextFreeLargerSubnet exercises the streaming nextFree walk over a
// /22 (1024 addresses): the network address is skipped, the gateway and an
// assigned host are taken, so the first free address is .3.
func TestBuildIPAMNextFreeLargerSubnet(t *testing.T) {
	networks := []Network{{ID: 1, CIDR: "10.0.0.0/22", Gateway: "10.0.0.1"}}
	hosts := []Host{{ID: 1, Name: "a", IPs: []string{"10.0.0.2"}}}
	u := BuildIPAM(networks, hosts, nil).Networks[0]
	if u.TotalUsable != 1022 { // 1024 - network - broadcast
		t.Errorf("TotalUsable = %d, want 1022", u.TotalUsable)
	}
	if u.NextFree != "10.0.0.3" {
		t.Errorf("NextFree = %q, want 10.0.0.3", u.NextFree)
	}
}

func TestBuildIPAMReservationSkippedByNextFree(t *testing.T) {
	networks := []Network{{ID: 1, Name: "lan", CIDR: "10.0.0.0/24"}}
	hosts := []Host{} // empty: first usable is .1
	res := []Reservation{{ID: 1, NetworkID: 1, Name: "r", StartIP: "10.0.0.1", EndIP: "10.0.0.3"}}
	u := BuildIPAM(networks, hosts, res).Networks[0]
	if u.NextFree != "10.0.0.4" {
		t.Fatalf("NextFree = %q, want 10.0.0.4 (skipping the reserved .1-.3)", u.NextFree)
	}
	if u.ReservedCount != 3 {
		t.Fatalf("ReservedCount = %d, want 3", u.ReservedCount)
	}
	// /24 has 254 usable; 0 used, 3 reserved -> 251 free.
	if u.FreeCount != 251 {
		t.Fatalf("FreeCount = %d, want 251", u.FreeCount)
	}
}

func TestBuildIPAMReservationOverlappingHostNotDoubleCounted(t *testing.T) {
	networks := []Network{{ID: 1, Name: "lan", CIDR: "10.0.0.0/24"}}
	hosts := []Host{{ID: 1, Name: "h", IPs: []string{"10.0.0.10"}}}
	res := []Reservation{{ID: 1, NetworkID: 1, Name: "r", StartIP: "10.0.0.10", EndIP: "10.0.0.11"}}
	u := BuildIPAM(networks, hosts, res).Networks[0]
	// .10 is both used and reserved; reserved-not-used is just .11 -> ReservedCount 1.
	if u.ReservedCount != 1 {
		t.Fatalf("ReservedCount = %d, want 1 (the .10 overlap is not double-counted)", u.ReservedCount)
	}
	// 254 usable - 1 used - 1 reserved-not-used = 252 free.
	if u.FreeCount != 252 {
		t.Fatalf("FreeCount = %d, want 252", u.FreeCount)
	}
}

func TestBuildIPAMReservationOnlyForItsNetwork(t *testing.T) {
	networks := []Network{{ID: 1, Name: "a", CIDR: "10.0.0.0/24"}, {ID: 2, Name: "b", CIDR: "10.0.1.0/24"}}
	res := []Reservation{{ID: 1, NetworkID: 2, Name: "r", StartIP: "10.0.1.1", EndIP: "10.0.1.9"}}
	report := BuildIPAM(networks, nil, res)
	if report.Networks[0].ReservedCount != 0 {
		t.Fatalf("network 1 should have 0 reserved, got %d", report.Networks[0].ReservedCount)
	}
	if report.Networks[1].ReservedCount != 9 {
		t.Fatalf("network 2 should have 9 reserved, got %d", report.Networks[1].ReservedCount)
	}
}

func TestBuildIPAMReservationStartingBelowNetwork(t *testing.T) {
	networks := []Network{{ID: 1, Name: "lan", CIDR: "10.0.0.0/24"}} // limit = 256 addresses
	// StartIP is 65536 addresses below the network base — far more than the /24's
	// walk bound of 256 — but the range still overlaps into 10.0.0.0/24 up to .5.
	res := []Reservation{{ID: 1, NetworkID: 1, Name: "r", StartIP: "9.255.0.0", EndIP: "10.0.0.5"}}
	u := BuildIPAM(networks, nil, res).Networks[0]
	// The walk is clamped to start at the network base (10.0.0.0), so the in-network
	// portion 10.0.0.0..10.0.0.5 (6 addresses) is reserved. Before the fix, the bounded
	// walk (limit = 256 increments) starting at 9.255.0.0 never got within 256 addresses
	// of 10.0.0.0, so it never reached the network at all and ReservedCount was 0.
	if u.ReservedCount != 6 {
		t.Fatalf("ReservedCount = %d, want 6 (in-network part of a reservation starting below the base)", u.ReservedCount)
	}
}
