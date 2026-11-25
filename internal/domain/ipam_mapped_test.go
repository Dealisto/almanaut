package domain

import (
	"reflect"
	"testing"
)

func TestIPAMMappedCapacityMatchesIPv4(t *testing.T) {
	for _, tc := range []struct {
		name, native, mapped, gateway string
		ips                           []string
		start, end                    string
	}{
		{name: "network address is unavailable", native: "192.0.2.0/24", mapped: "::ffff:192.0.2.0/120"},
		{name: "full subnet must not suggest broadcast", native: "192.0.2.0/30", mapped: "::ffff:192.0.2.0/126", gateway: "192.0.2.1", ips: []string{"192.0.2.2"}},
		{name: "reservation excludes endpoints", native: "192.0.2.0/30", mapped: "::ffff:192.0.2.0/126", start: "192.0.2.0", end: "192.0.2.3"},
		{name: "hosts at subnet endpoints", native: "192.0.2.0/30", mapped: "::ffff:192.0.2.0/126", ips: []string{"192.0.2.0", "192.0.2.3"}},
		{name: "point to point", native: "192.0.2.0/31", mapped: "::ffff:192.0.2.0/127"},
		{name: "point to point reservation", native: "192.0.2.0/31", mapped: "::ffff:192.0.2.0/127", start: "192.0.2.0", end: "192.0.2.0"},
		{name: "host route", native: "192.0.2.1/32", mapped: "::ffff:192.0.2.1/128"},
		{name: "reserved host route", native: "192.0.2.1/32", mapped: "::ffff:192.0.2.1/128", start: "192.0.2.1", end: "192.0.2.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reservations []Reservation
			if tc.start != "" {
				reservations = []Reservation{{NetworkID: 1, StartIP: tc.start, EndIP: tc.end}}
			}
			hosts := []Host{{ID: 1, IPs: tc.ips}}
			native := BuildIPAM([]Network{{ID: 1, CIDR: tc.native, Gateway: tc.gateway}}, hosts, reservations).Networks[0]
			mapped := BuildIPAM([]Network{{ID: 1, CIDR: tc.mapped, Gateway: tc.gateway}}, hosts, reservations).Networks[0]
			mapped.Network = native.Network // spelling is the only expected difference
			if !reflect.DeepEqual(mapped, native) {
				t.Fatalf("mapped usage differs from IPv4: mapped=%+v native=%+v", mapped, native)
			}
		})
	}
}

func TestIPAMMappedNetworksUseEffectiveSpecificity(t *testing.T) {
	for _, networks := range [][]Network{
		{{ID: 1, CIDR: "::ffff:192.0.2.0/120"}, {ID: 2, CIDR: "192.0.2.128/25"}},
		{{ID: 2, CIDR: "192.0.2.128/25"}, {ID: 1, CIDR: "::ffff:192.0.2.0/120"}},
	} {
		hosts := []Host{{ID: 1, Name: "router", IPs: []string{"192.0.2.129", "::ffff:192.0.2.130"}}}
		report := BuildIPAM(networks, hosts, nil)
		for _, usage := range report.Networks {
			want := 0
			if usage.Network.ID == 2 {
				want = 2
			}
			if usage.UsedCount != want {
				t.Errorf("network %s has %d addresses, want %d", usage.Network.CIDR, usage.UsedCount, want)
			}
			single, ok := BuildNetworkUsage(usage.Network.ID, networks, hosts, nil)
			if !ok || !reflect.DeepEqual(single, usage) {
				t.Errorf("single-network usage differs: %+v / %+v", single, usage)
			}
		}
	}
}

func TestIPAMMappedEquivalentNetworksOverlapAndKeepFirstTie(t *testing.T) {
	networks := []Network{
		{ID: 1, CIDR: "192.0.2.0/24"},
		{ID: 2, CIDR: "::ffff:192.0.2.0/120"},
		{ID: 3, CIDR: "::ffff:192.0.2.128/121"},
	}
	conflicts := BuildIPAMConflicts(networks, nil)
	if len(conflicts.Overlaps) != 1 || conflicts.Overlaps[0].A.ID != 1 || conflicts.Overlaps[0].B.ID != 2 {
		t.Fatalf("equivalent IPv4 and mapped blocks must overlap: %+v", conflicts.Overlaps)
	}
	for _, target := range networks[:2] {
		got := NetworkOverlaps(target, networks)
		if len(got) != 1 || got[0].ID == target.ID || got[0].ID == 3 {
			t.Errorf("network %d overlap = %+v", target.ID, got)
		}
	}
	report := BuildIPAM(networks, []Host{{ID: 1, IPs: []string{"192.0.2.1"}}}, nil)
	if report.Networks[0].UsedCount != 1 || report.Networks[1].UsedCount != 0 {
		t.Fatalf("equal blocks must retain first-network tie breaking: %+v", report.Networks)
	}
}
