package domain

import (
	"math/rand/v2"
	"net"
	"net/netip"
	"reflect"
	"testing"
)

// linearNextFree is intentionally independent of the range-skip implementation:
// it enumerates a bounded subnet and checks each original interval separately.
func linearNextFree(ipn *net.IPNet, taken map[string]bool, reserved networkReservations, allocated map[string]bool) string {
	ones, bits := ipn.Mask.Size()
	if bits-ones > maxEnumerableHostBits {
		return ""
	}
	addr, _ := netip.AddrFromSlice(ipn.IP.Mask(ipn.Mask))
	count := 1 << (bits - ones)
	for offset := 0; offset < count; offset++ {
		if ipn.IP.To4() != nil && bits-ones >= 2 && (offset == 0 || offset == count-1) {
			addr = addr.Next()
			continue
		}
		candidate := addr.Unmap().String()
		blocked := taken[candidate] || allocated[candidate]
		ranges := reserved.v6
		if addr.Is4() || addr.Is4In6() {
			ranges = reserved.v4
		}
		value := netip.AddrFrom16(addr.As16())
		for _, span := range ranges {
			start, _ := netip.AddrFromSlice(span.start)
			end, _ := netip.AddrFromSlice(span.end)
			if start.Compare(value) <= 0 && end.Compare(value) >= 0 {
				blocked = true
				break
			}
		}
		if !blocked {
			return candidate
		}
		addr = addr.Next()
	}
	return ""
}

func TestNextFreeMatchesBoundedEnumeration(t *testing.T) {
	rng := rand.New(rand.NewPCG(31, 72))
	for _, cidr := range []string{
		"10.0.0.0/24", "10.0.0.0/31", "10.0.0.1/32", "255.255.255.248/29",
		"::ffff:10.0.0.0/120", "::ffff:255.255.255.254/127", "::ffff:255.255.255.255/128",
		"2001:db8::/120", "2001:db8::/127", "2001:db8::/128",
		"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ff00/120", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128",
	} {
		t.Run(cidr, func(t *testing.T) {
			_, ipn, _ := net.ParseCIDR(cidr)
			ones, bits := ipn.Mask.Size()
			addresses := make([]net.IP, 1<<(bits-ones))
			addr, _ := netip.AddrFromSlice(ipn.IP.Mask(ipn.Mask))
			for i := range addresses {
				value := addr.As16()
				addresses[i] = append(net.IP(nil), value[:]...)
				addr = addr.Next()
			}
			for trial := 0; trial < 250; trial++ {
				taken, allocated := map[string]bool{}, map[string]bool{}
				for _, ip := range addresses {
					if rng.IntN(3) == 0 {
						taken[ip.String()] = true
					}
					if rng.IntN(3) == 0 {
						allocated[ip.String()] = true
					}
				}
				var spans ipRanges
				for range rng.IntN(12) {
					a, b := rng.IntN(len(addresses)), rng.IntN(len(addresses))
					spans = append(spans, ipRange{addresses[min(a, b)], addresses[max(a, b)]})
				}
				original := networkReservations{v6: spans}
				if ipn.IP.To4() != nil {
					original = networkReservations{v4: spans}
				}
				want := linearNextFree(ipn, taken, original, allocated)
				merged := mergeIPRanges(append(ipRanges(nil), spans...))
				reserved := networkReservations{v6: merged}
				if ipn.IP.To4() != nil {
					reserved = networkReservations{v4: merged}
				}
				before := make(ipRanges, len(merged))
				for i, span := range merged {
					before[i] = ipRange{append(net.IP(nil), span.start...), append(net.IP(nil), span.end...)}
				}
				if got := nextFree(ipn, taken, reserved, allocated); got != want {
					t.Fatalf("trial %d: next free = %q, enumeration = %q", trial, got, want)
				}
				if len(before) > 0 && !reflect.DeepEqual(before, merged) {
					t.Fatal("next-free search mutated reservation ranges")
				}
			}
		})
	}
}

func TestNextFreeSkipsRangesCrossingSubnetBounds(t *testing.T) {
	for _, tc := range []struct {
		cidr, start, end, want string
	}{
		{"10.0.0.0/24", "9.255.255.255", "10.0.0.127", "10.0.0.128"},
		{"10.0.0.0/24", "9.255.255.255", "10.0.1.0", ""},
		{"::ffff:10.0.0.0/120", "9.255.255.255", "10.0.1.0", ""},
		{"2001:db8::/120", "2001:db7:ffff:ffff:ffff:ffff:ffff:ffff", "2001:db8::ff", ""},
		{"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ff00/120", "::", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", ""},
		{"10.0.0.0/15", "10.0.0.0", "10.0.0.1", ""},
	} {
		t.Run(tc.cidr+"/"+tc.end, func(t *testing.T) {
			_, ipn, _ := net.ParseCIDR(tc.cidr)
			spans := ipRanges{{net.ParseIP(tc.start), net.ParseIP(tc.end)}}
			reserved := networkReservations{v6: spans}
			if ipn.IP.To4() != nil {
				reserved = networkReservations{v4: spans}
			}
			if got := nextFree(ipn, nil, reserved, nil); got != tc.want {
				t.Fatalf("next free = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIPAMGlobalRangeMergePreservesLocalCapacity(t *testing.T) {
	networks := []Network{
		{ID: 1, CIDR: "10.0.0.0/28", Gateway: "10.0.0.1"},
		{ID: 2, CIDR: "10.0.0.0/29"},
	}
	reservations := []Reservation{
		{NetworkID: 1, StartIP: "10.0.0.2", EndIP: "10.0.0.4"},
		{NetworkID: 2, StartIP: "10.0.0.4", EndIP: "10.0.0.6"},
	}
	report := BuildIPAM(networks, nil, reservations)
	for i, want := range []struct {
		reserved, free int
		next           string
	}{{4, 10, "10.0.0.8"}, {3, 3, ""}} {
		got := report.Networks[i]
		if got.ReservedCount != want.reserved || got.FreeCount != want.free || got.NextFree != want.next {
			t.Fatalf("network %d: got %+v, want reserved=%d free=%d next=%q", i, got, want.reserved, want.free, want.next)
		}
		single, ok := BuildNetworkUsage(networks[i].ID, networks, nil, reservations)
		if !ok || !reflect.DeepEqual(single, got) {
			t.Fatalf("single-network usage differs: %+v / %+v", single, got)
		}
	}
}
