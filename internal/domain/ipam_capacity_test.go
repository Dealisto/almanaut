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
		{name: "IPv4 reservations in mapped IPv6 subnet", cidr: "::ffff:192.0.2.0/120", start: "192.0.2.0", end: "192.0.2.10", free: 244, reserved: 10, next: "192.0.2.11"},
		{name: "mapped reservations in IPv4 subnet", cidr: "192.0.2.0/24", start: "::ffff:192.0.2.0", end: "::ffff:192.0.2.10", free: 244, reserved: 10, next: "192.0.2.11"},
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

func TestIPAMCountsReservationsWithoutEnumeratingLargeNetworks(t *testing.T) {
	for _, tc := range []struct {
		name, cidr, gateway string
		ips                 []string
		ranges              [][2]string
		free, reserved      int
	}{
		{
			name: "large IPv4 overlapping ranges and occupied addresses", cidr: "10.0.0.0/15", gateway: "10.0.0.1",
			ips:    []string{"10.0.0.2", "10.0.1.0"},
			ranges: [][2]string{{"10.0.0.0", "10.0.0.10"}, {"10.0.0.5", "10.0.0.20"}},
			free:   131049, reserved: 19,
		},
		{
			name: "IPv4 billion-address reservation clipped at both ends", cidr: "64.0.0.0/2",
			ranges:   [][2]string{{"0.0.0.0", "255.255.255.255"}},
			reserved: (1 << 30) - 2,
		},
		{
			name: "IPv6 billion-address reservation", cidr: "2001:db8::/98", gateway: "2001:db8::1",
			ips:      []string{"2001:db8::1"},
			ranges:   [][2]string{{"2001:db8::", "2001:db8::3fff:ffff"}},
			reserved: (1 << 30) - 1,
		},
		{
			name: "ignore reversed unrelated and wrong-family ranges", cidr: "10.0.0.0/15",
			ranges: [][2]string{{"10.0.0.10", "10.0.0.5"}, {"11.0.0.1", "11.0.0.10"}, {"::", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"}},
			free:   131070,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			networks := []Network{{ID: 1, CIDR: tc.cidr, Gateway: tc.gateway}}
			hosts := []Host{{ID: 1, IPs: tc.ips}}
			var reservations []Reservation
			for _, span := range tc.ranges {
				reservations = append(reservations, Reservation{NetworkID: 1, StartIP: span[0], EndIP: span[1]})
			}
			got := BuildIPAM(networks, hosts, reservations).Networks[0]
			if got.Unbounded || got.FreeCount != tc.free || got.ReservedCount != tc.reserved {
				t.Fatalf("usage = %+v, want free %d, reserved %d", got, tc.free, tc.reserved)
			}
			if got.NextFree != "" {
				t.Fatalf("large networks must retain the next-free enumeration cap, got %q", got.NextFree)
			}
			one, ok := BuildNetworkUsage(1, networks, hosts, reservations)
			if !ok || !reflect.DeepEqual(one, got) {
				t.Fatalf("single-network view differs: %+v / %+v", one, got)
			}
		})
	}
}

func TestIPAMMergesNestedAndOverlappingReservations(t *testing.T) {
	networks := []Network{{ID: 1, CIDR: "10.0.0.0/24"}}
	reservations := []Reservation{
		{NetworkID: 1, StartIP: "10.0.0.5", EndIP: "10.0.0.20"},
		{NetworkID: 1, StartIP: "10.0.0.0", EndIP: "10.0.0.10"},
		{NetworkID: 1, StartIP: "10.0.0.6", EndIP: "10.0.0.7"},
		{NetworkID: 1, StartIP: "10.0.0.21", EndIP: "10.0.0.30"},
	}
	got := BuildIPAM(networks, nil, reservations).Networks[0]
	if got.ReservedCount != 30 || got.FreeCount != 224 || got.NextFree != "10.0.0.31" {
		t.Fatalf("overlapping and adjacent ranges = %+v", got)
	}
}

func TestIPAMGroupsReservationsWithoutChangingOrderOrAttribution(t *testing.T) {
	networks := []Network{{ID: 1, CIDR: "10.0.0.0/24"}, {ID: 2, CIDR: "10.0.0.0/25"}, {ID: 3, CIDR: "10.0.1.0/24"}}
	reservations := []Reservation{
		{ID: 1, NetworkID: 2, StartIP: "10.0.0.5", EndIP: "10.0.0.5"},
		{ID: 2, NetworkID: 1, StartIP: "10.0.0.1", EndIP: "10.0.0.2"},
		{ID: 3, NetworkID: 99, StartIP: "10.0.0.3", EndIP: "10.0.0.3"},
		{ID: 4, NetworkID: 2, StartIP: "10.0.0.1", EndIP: "10.0.0.1"},
	}
	original := append([]Reservation(nil), reservations...)
	report := BuildIPAM(networks, nil, reservations)
	for _, usage := range report.Networks {
		single, ok := BuildNetworkUsage(usage.Network.ID, networks, nil, reservations)
		if !ok || !reflect.DeepEqual(single, usage) {
			t.Fatalf("grouped full report differs from individual view: %+v / %+v", usage, single)
		}
	}
	if len(report.Networks[1].Reservations) != 2 || report.Networks[1].Reservations[0].ID != 1 || report.Networks[1].Reservations[1].ID != 4 {
		t.Fatalf("reservation input order changed: %+v", report.Networks[1].Reservations)
	}
	report.Networks[1].Reservations[0].Name = "changed in report"
	if !reflect.DeepEqual(reservations, original) {
		t.Fatal("report reservations share caller-owned storage")
	}
}
