package domain

import (
	"reflect"
	"testing"
)

func TestIPAMFreeCapacityMatchesAvailableAddresses(t *testing.T) {
	for _, tc := range []struct {
		name           string
		cidr           string
		gateway        string
		hostIPs        []string
		start, end     string
		free, reserved int
		next           string
	}{
		{name: "network reservation", cidr: "10.0.0.0/30", start: "10.0.0.0", end: "10.0.0.0", free: 2, next: "10.0.0.1"},
		{name: "broadcast reservation", cidr: "10.0.0.0/30", start: "10.0.0.3", end: "10.0.0.3", free: 2, next: "10.0.0.1"},
		{name: "reservation includes network", cidr: "10.0.0.0/30", start: "10.0.0.0", end: "10.0.0.1", free: 1, reserved: 1, next: "10.0.0.2"},
		{name: "host uses subnet endpoints", cidr: "10.0.0.0/30", hostIPs: []string{"10.0.0.0", "10.0.0.3"}, free: 2, next: "10.0.0.1"},
		{name: "gateway fills last address", cidr: "10.0.0.0/30", gateway: "10.0.0.1", hostIPs: []string{"10.0.0.2"}, reserved: 1},
		{name: "gateway overlaps reservation", cidr: "10.0.0.0/30", gateway: "10.0.0.1", start: "10.0.0.1", end: "10.0.0.1", free: 1, reserved: 1, next: "10.0.0.2"},
		{name: "gateway overlaps host", cidr: "10.0.0.0/30", gateway: "10.0.0.1", hostIPs: []string{"10.0.0.1"}, free: 1, next: "10.0.0.2"},
		{name: "gateway outside subnet", cidr: "10.0.0.0/30", gateway: "10.0.1.1", free: 2, next: "10.0.0.1"},
		{name: "gateway at network address", cidr: "10.0.0.0/30", gateway: "10.0.0.0", free: 2, next: "10.0.0.1"},
		{name: "point to point reservation", cidr: "10.0.0.0/31", start: "10.0.0.0", end: "10.0.0.0", free: 1, reserved: 1, next: "10.0.0.1"},
		{name: "host route reservation", cidr: "10.0.0.1/32", start: "10.0.0.1", end: "10.0.0.1", reserved: 1},
		{name: "IPv6 subnet base remains usable", cidr: "2001:db8::/126", start: "2001:db8::", end: "2001:db8::", free: 3, reserved: 1, next: "2001:db8::1"},
		{name: "IPv6 gateway overlaps host", cidr: "2001:db8::/126", gateway: "2001:db8::1", hostIPs: []string{"2001:db8::1"}, free: 3, next: "2001:db8::"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			networks := []Network{{ID: 1, CIDR: tc.cidr, Gateway: tc.gateway}}
			hosts := []Host{{ID: 1, IPs: tc.hostIPs}}
			var reservations []Reservation
			if tc.start != "" {
				reservations = []Reservation{{NetworkID: 1, StartIP: tc.start, EndIP: tc.end}}
			}
			got := BuildIPAM(networks, hosts, reservations).Networks[0]
			if got.FreeCount != tc.free || got.ReservedCount != tc.reserved || got.NextFree != tc.next {
				t.Errorf("free/reserved/next = %d/%d/%q, want %d/%d/%q", got.FreeCount, got.ReservedCount, got.NextFree, tc.free, tc.reserved, tc.next)
			}
			one, ok := BuildNetworkUsage(1, networks, hosts, reservations)
			if !ok || !reflect.DeepEqual(one, got) {
				t.Errorf("single-network view differs from full report: %+v / %+v", one, got)
			}
		})
	}
}

func TestIPAMRepeatedHostIPsDoNotSelfConflict(t *testing.T) {
	networks := []Network{{ID: 1, CIDR: "2001:db8::/64"}, {ID: 2, CIDR: "2001:db8::/126"}}
	hosts := []Host{{ID: 1, Name: "nas", IPs: []string{"2001:db8::1", "2001:db8:0:0:0:0:0:1", "192.0.2.1", "192.0.2.1"}}}
	report := BuildIPAM(networks, hosts, nil)
	if report.Networks[0].UsedCount != 0 || len(report.Networks[1].Used) != 1 || len(report.Networks[1].Conflicts) != 0 || len(report.Unassigned) != 1 {
		t.Fatalf("host repeats must not duplicate allocations or self-conflict: %+v", report)
	}
	hosts = append(hosts, Host{ID: 2, Name: "router", IPs: []string{"2001:db8::1"}})
	u := BuildIPAM(networks, hosts, nil).Networks[1]
	if len(u.Conflicts) != 1 || len(u.Conflicts[0]) != 2 {
		t.Fatalf("different hosts must still conflict exactly once each: %+v", u.Conflicts)
	}
}
