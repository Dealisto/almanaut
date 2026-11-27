package domain

import (
	"reflect"
	"testing"
)

func TestIPAMNextFreeExcludesHostsAttributedToOtherNetworks(t *testing.T) {
	for _, tc := range []struct {
		name, parent, child, next string
		ips                       []string
	}{
		{"nested IPv4", "10.0.0.0/24", "10.0.0.0/25", "10.0.0.2", []string{"10.0.0.1"}},
		{"equivalent block", "10.0.0.0/24", "10.0.0.0/24", "10.0.0.2", []string{"10.0.0.1"}},
		{"mapped host", "10.0.0.0/24", "10.0.0.0/25", "10.0.0.2", []string{"::ffff:10.0.0.1"}},
		{"mapped parent", "::ffff:10.0.0.0/120", "10.0.0.0/25", "10.0.0.2", []string{"10.0.0.1"}},
		{"mapped child", "10.0.0.0/24", "::ffff:10.0.0.0/121", "10.0.0.2", []string{"10.0.0.1"}},
		{"nested IPv6", "2001:db8::/125", "2001:db8::/126", "2001:db8::2", []string{"2001:db8::", "2001:db8:0:0:0:0:0:1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// First input wins equal-prefix ties; both cases must exclude its IPs.
			networks := []Network{{ID: 2, CIDR: tc.child}, {ID: 1, CIDR: tc.parent}}
			hosts := []Host{{ID: 1, Name: "child host", IPs: tc.ips}}
			report := BuildIPAM(networks, hosts, nil)
			got := report.Networks[1]
			if got.NextFree != tc.next {
				t.Fatalf("next free = %q, want %q", got.NextFree, tc.next)
			}
			if got.UsedCount != 0 || len(got.Used) != 0 || report.Networks[0].UsedCount != len(tc.ips) {
				t.Fatalf("suggestions changed host attribution: %+v", report.Networks)
			}
			if got.FreeCount != got.TotalUsable || got.ReservedCount != 0 {
				t.Fatalf("suggestions changed local capacity: %+v", got)
			}
			single, ok := BuildNetworkUsage(1, networks, hosts, nil)
			if !ok || !reflect.DeepEqual(single, got) {
				t.Fatalf("single-network report differs: %+v / %+v", single, got)
			}
		})
	}
}

func TestIPAMNextFreeCombinesOtherHostsWithLocalReservations(t *testing.T) {
	for _, tc := range []struct {
		name, parent, child, host, gateway, reserved, next string
	}{
		{"IPv4", "10.0.0.0/29", "10.0.0.0/30", "10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"},
		{"mapped IPv4", "::ffff:10.0.0.0/125", "10.0.0.0/30", "::ffff:10.0.0.1", "10.0.0.2", "::ffff:10.0.0.3", "10.0.0.4"},
		{"IPv6", "2001:db8::/126", "2001:db8::/127", "2001:db8::", "2001:db8::1", "2001:db8::2", "2001:db8::3"},
		{"full IPv6", "2001:db8::/126", "2001:db8::/127", "2001:db8::", "2001:db8::1", "2001:db8::2", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			networks := []Network{{ID: 1, CIDR: tc.parent, Gateway: tc.gateway}, {ID: 2, CIDR: tc.child}}
			hosts := []Host{{ID: 1, IPs: []string{tc.host}}}
			end := tc.reserved
			if tc.next == "" {
				end = "2001:db8::3"
			}
			reservations := []Reservation{{NetworkID: 1, StartIP: tc.reserved, EndIP: end}}
			got := BuildIPAM(networks, hosts, reservations).Networks[0]
			if got.NextFree != tc.next {
				t.Fatalf("next free = %q, want %q", got.NextFree, tc.next)
			}
			single, ok := BuildNetworkUsage(1, networks, hosts, reservations)
			if !ok || !reflect.DeepEqual(single, got) {
				t.Fatalf("single-network report differs: %+v / %+v", single, got)
			}
		})
	}
}

func TestIPAMNextFreeExcludesOtherNetworkReservations(t *testing.T) {
	for _, tc := range []struct {
		name, target, other, gateway, start, end, next string
	}{
		{"child IPv4", "10.0.0.0/29", "10.0.0.0/30", "10.0.0.1", "10.0.0.2", "10.0.0.2", "10.0.0.4"},
		{"parent IPv4", "10.0.0.0/30", "10.0.0.0/29", "10.0.0.1", "10.0.0.2", "10.0.0.3", ""},
		{"equivalent IPv4", "10.0.0.0/29", "10.0.0.0/29", "10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"},
		{"mapped IPv4", "::ffff:10.0.0.0/125", "10.0.0.0/30", " ::ffff:10.0.0.1 ", " ::ffff:10.0.0.2", "10.0.0.2 ", "10.0.0.4"},
		{"child IPv6", "2001:db8::/125", "2001:db8::/126", "2001:db8::", "2001:db8::1", "2001:db8::2", "2001:db8::3"},
		{"unbounded parent IPv6", "2001:db8::/126", "2001:db8::/64", "2001:db8::", "2001:db8::1", "2001:db8::ffff:ffff", ""},
		{"clipped IPv4 range", "10.0.0.0/29", "10.0.0.0/30", "", "10.0.0.0", "10.0.0.7", "10.0.0.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			networks := []Network{{ID: 1, CIDR: tc.target}, {ID: 2, CIDR: tc.other, Gateway: tc.gateway}}
			reservations := []Reservation{{NetworkID: 2, StartIP: tc.start, EndIP: tc.end}}
			got := BuildIPAM(networks, nil, reservations).Networks[0]
			if got.NextFree != tc.next {
				t.Fatalf("next free = %q, want %q", got.NextFree, tc.next)
			}
			if got.FreeCount != got.TotalUsable || got.ReservedCount != 0 || len(got.Reservations) != 0 {
				t.Fatalf("other network changed local capacity: %+v", got)
			}
			single, ok := BuildNetworkUsage(1, networks, nil, reservations)
			if !ok || !reflect.DeepEqual(single, got) {
				t.Fatalf("single-network report differs: %+v / %+v", single, got)
			}
		})
	}
}

func TestIPAMNextFreeIgnoresReservationsWithoutApplicableNetwork(t *testing.T) {
	networks := []Network{
		{ID: 1, CIDR: "10.0.0.0/29"},
		{ID: 2, CIDR: "10.1.0.0/24", Gateway: "10.0.0.1"},
		{ID: 3, CIDR: "invalid", Gateway: "10.0.0.1"},
	}
	reservations := []Reservation{
		{NetworkID: 0, StartIP: "10.0.0.1", EndIP: "10.0.0.6"},
		{NetworkID: 2, StartIP: "10.0.0.1", EndIP: "10.0.0.6"},
		{NetworkID: 3, StartIP: "10.0.0.1", EndIP: "10.0.0.6"},
		{NetworkID: 99, StartIP: "10.0.0.1", EndIP: "10.0.0.6"},
	}
	got := BuildIPAM(networks, nil, reservations).Networks[0]
	if got.NextFree != "10.0.0.1" {
		t.Fatalf("unrelated reservations blocked free address: %+v", got)
	}
}

func TestIPAMNextFreeKeepsReservationAddressFamiliesSeparate(t *testing.T) {
	networks := []Network{
		{ID: 1, CIDR: "10.0.0.0/29"},
		{ID: 2, CIDR: "::/0"},
	}
	reservations := []Reservation{{NetworkID: 2, StartIP: "::", EndIP: "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"}}
	got := BuildIPAM(networks, nil, reservations).Networks[0]
	if got.NextFree != "10.0.0.1" {
		t.Fatalf("native IPv6 reservation blocked IPv4 address: %+v", got)
	}
}

func TestIPAMNextFreeExcludesOtherNetworkEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name, child, next string
		ips               []string
	}{
		{"network address", "10.0.0.4/30", "10.0.0.5", []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}},
		{"broadcast address", "10.0.0.4/30", "10.0.0.8", []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.5", "10.0.0.6"}},
		{"mapped network address", "::ffff:10.0.0.4/126", "10.0.0.5", []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}},
		{"point to point", "10.0.0.2/31", "10.0.0.2", []string{"10.0.0.1"}},
		{"single address", "10.0.0.1/32", "10.0.0.1", nil},
		{"mapped point to point", "::ffff:10.0.0.2/127", "10.0.0.2", []string{"10.0.0.1"}},
		{"mapped single address", "::ffff:10.0.0.1/128", "10.0.0.1", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			networks := []Network{{ID: 1, CIDR: "10.0.0.0/28"}, {ID: 2, CIDR: tc.child}}
			hosts := []Host{{ID: 1, IPs: tc.ips}}
			got := BuildIPAM(networks, hosts, nil).Networks[0]
			if got.NextFree != tc.next {
				t.Fatalf("next free = %q, want %q", got.NextFree, tc.next)
			}
			if got.TotalUsable != 14 || got.ReservedCount != 0 || got.FreeCount != got.TotalUsable-got.UsedCount {
				t.Fatalf("other network endpoints changed local capacity: %+v", got)
			}
			single, ok := BuildNetworkUsage(1, networks, hosts, nil)
			if !ok || !reflect.DeepEqual(single, got) {
				t.Fatalf("single-network report differs: %+v / %+v", single, got)
			}
		})
	}
}
