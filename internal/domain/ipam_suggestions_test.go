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
