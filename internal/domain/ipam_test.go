package domain

import "testing"

func TestBuildIPAMOccupancy(t *testing.T) {
	networks := []Network{{ID: 1, Name: "lan", CIDR: "192.168.1.0/24", Gateway: "192.168.1.1"}}
	hosts := []Host{
		{ID: 10, Name: "nas", IPs: []string{"192.168.1.5"}},
		{ID: 11, Name: "pi", IPs: []string{"192.168.1.10", "10.0.0.2"}},
	}
	report := BuildIPAM(networks, hosts)
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
	report := BuildIPAM(networks, hosts)
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
	u := BuildIPAM(networks, hosts).Networks[0]
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
	u := BuildIPAM(networks, hosts).Networks[0]
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
	u := BuildIPAM(networks, hosts).Networks[0]
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
	u := BuildIPAM(networks, hosts).Networks[0]
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
	report := BuildIPAM(networks, hosts)
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
	u := BuildIPAM(networks, hosts).Networks[0]
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
	u := BuildIPAM(networks, nil).Networks[0]
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
	u := BuildIPAM(networks, hosts).Networks[0]
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

func TestBuildIPAMNextFreeSkipsGateway(t *testing.T) {
	// The gateway is reserved: NextFree must not suggest it.
	networks := []Network{{ID: 1, CIDR: "192.168.1.0/24", Gateway: "192.168.1.1"}}
	hosts := []Host{{ID: 1, Name: "a", IPs: []string{"192.168.1.5"}}}
	u := BuildIPAM(networks, hosts).Networks[0]
	if u.NextFree != "192.168.1.2" {
		t.Errorf("NextFree = %q, want 192.168.1.2 (gateway .1 skipped)", u.NextFree)
	}
}
