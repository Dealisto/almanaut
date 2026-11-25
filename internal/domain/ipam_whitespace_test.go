package domain

import (
	"reflect"
	"testing"
)

func TestIPAMHonorsValidatedPaddedAddresses(t *testing.T) {
	for _, tc := range []struct {
		name, cidr, reserved, next string
	}{
		{"IPv4", "10.0.0.0/30", "10.0.0.1", "10.0.0.2"},
		{"IPv6", "2001:db8::/127", "2001:db8::", "2001:db8::1"},
	} {
		for _, gateway := range []bool{false, true} {
			name := tc.name + "/reservation"
			if gateway {
				name = tc.name + "/gateway"
			}
			t.Run(name, func(t *testing.T) {
				n := Network{ID: 1, Name: "lan", CIDR: tc.cidr}
				var reservations []Reservation
				if gateway {
					n.Gateway = " \t" + tc.reserved + "\n"
				} else {
					r := Reservation{ID: 1, NetworkID: 1, Name: "reserved", StartIP: " " + tc.reserved, EndIP: tc.reserved + "\t"}
					if err := r.Validate(); err != nil {
						t.Fatal(err)
					}
					reservations = []Reservation{r}
				}
				if err := n.Validate(); err != nil {
					t.Fatal(err)
				}
				usage := BuildIPAM([]Network{n}, nil, reservations).Networks[0]
				if usage.NextFree != tc.next || usage.ReservedCount != 1 || usage.FreeCount != 1 {
					t.Fatalf("validated address must remain unavailable: %+v", usage)
				}
				single, ok := BuildNetworkUsage(n.ID, []Network{n}, nil, reservations)
				if !ok || !reflect.DeepEqual(single, usage) {
					t.Fatalf("detail usage differs from full report: %+v", single)
				}
			})
		}
	}
}
