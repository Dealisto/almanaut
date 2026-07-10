package domain

import "testing"

func TestIPAMConflictsDuplicateIPs(t *testing.T) {
	hosts := []Host{
		{ID: 1, Name: "a", IPs: []string{"192.168.1.5", "10.0.0.1"}},
		{ID: 2, Name: "b", IPs: []string{"192.168.1.5"}},          // collides with a on .5
		{ID: 3, Name: "c", IPs: []string{"192.168.1.5"}},          // three-way collision
		{ID: 4, Name: "d", IPs: []string{"10.0.0.9", "10.0.0.9"}}, // same host twice: not a conflict
	}
	c := BuildIPAMConflicts(nil, hosts)
	if len(c.DuplicateIPs) != 1 {
		t.Fatalf("DuplicateIPs = %+v, want one group", c.DuplicateIPs)
	}
	d := c.DuplicateIPs[0]
	if d.IP != "192.168.1.5" || len(d.Hosts) != 3 {
		t.Fatalf("conflict = %+v, want .5 shared by 3 hosts", d)
	}
	// hosts sorted by name: a, b, c
	if d.Hosts[0].HostName != "a" || d.Hosts[2].HostName != "c" {
		t.Errorf("hosts not sorted by name: %+v", d.Hosts)
	}
}

func TestIPAMConflictsOutsideNetworks(t *testing.T) {
	networks := []Network{{ID: 1, CIDR: "192.168.1.0/24"}}
	hosts := []Host{
		{ID: 1, Name: "in", IPs: []string{"192.168.1.5"}},
		{ID: 2, Name: "out", IPs: []string{"10.9.9.9"}},
	}
	c := BuildIPAMConflicts(networks, hosts)
	if len(c.OutsideNetworks) != 1 || c.OutsideNetworks[0].IP != "10.9.9.9" {
		t.Errorf("OutsideNetworks = %+v, want [10.9.9.9]", c.OutsideNetworks)
	}
}

func TestIPAMConflictsOverlapDuplicateBlock(t *testing.T) {
	networks := []Network{
		{ID: 1, Name: "lan", CIDR: "192.168.1.0/24"},
		{ID: 2, Name: "lan-dup", CIDR: "192.168.1.0/24"}, // identical block -> conflict
		{ID: 3, Name: "other", CIDR: "192.168.2.0/24"},   // disjoint -> fine
	}
	c := BuildIPAMConflicts(networks, nil)
	if len(c.Overlaps) != 1 {
		t.Fatalf("Overlaps = %+v, want one", c.Overlaps)
	}
	o := c.Overlaps[0]
	if !((o.A.ID == 1 && o.B.ID == 2) || (o.A.ID == 2 && o.B.ID == 1)) {
		t.Errorf("overlap pair = %d,%d, want 1&2", o.A.ID, o.B.ID)
	}
}

func TestIPAMConflictsNoFalsePositiveForSubnet(t *testing.T) {
	// A more-specific subnet inside a broader one is a legitimate parent/child
	// and must not be flagged.
	networks := []Network{
		{ID: 1, Name: "big", CIDR: "10.0.0.0/16"},
		{ID: 2, Name: "small", CIDR: "10.0.1.0/24"},
	}
	c := BuildIPAMConflicts(networks, nil)
	if len(c.Overlaps) != 0 {
		t.Errorf("Overlaps = %+v, want none for parent/child", c.Overlaps)
	}
}

func TestNetworkOverlaps(t *testing.T) {
	networks := []Network{
		{ID: 1, Name: "lan", CIDR: "10.0.0.0/24"},
		{ID: 2, Name: "lan-dup", CIDR: "10.0.0.0/24"},
		{ID: 3, Name: "parent", CIDR: "10.0.0.0/16"}, // parent/child, excluded
		{ID: 4, Name: "other", CIDR: "10.1.0.0/24"},
	}
	got := NetworkOverlaps(networks[0], networks)
	if len(got) != 1 || got[0].ID != 2 {
		t.Errorf("NetworkOverlaps = %+v, want [lan-dup]", got)
	}
}

func TestIPAMConflictsAny(t *testing.T) {
	if (IPAMConflicts{}).Any() {
		t.Error("empty conflicts should report Any() == false")
	}
	if !(IPAMConflicts{Overlaps: []NetworkOverlap{{}}}).Any() {
		t.Error("non-empty conflicts should report Any() == true")
	}
}
