package domain

import (
	"fmt"
	"testing"
)

func BenchmarkIPAMOverlapsDistinctNetworks(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			networks := make([]Network, count)
			for i := range networks {
				networks[i] = Network{ID: int64(i + 1), CIDR: fmt.Sprintf("10.%d.%d.0/24", i/256, i%256)}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if overlaps := overlappingNetworks(networks); len(overlaps) != 0 {
					b.Fatal("distinct blocks overlap")
				}
			}
		})
	}
}

func BenchmarkBuildIPAMManyReservations(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			networks := make([]Network, count)
			reservations := make([]Reservation, 0, count*10)
			for i := range networks {
				prefix := fmt.Sprintf("10.%d.%d", i/256, i%256)
				networks[i] = Network{ID: int64(i + 1), CIDR: prefix + ".0/24"}
				for j := range 10 {
					reservations = append(reservations, Reservation{NetworkID: int64(i + 1), StartIP: fmt.Sprintf("%s.%d", prefix, j+10), EndIP: fmt.Sprintf("%s.%d", prefix, j+10)})
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				report := BuildIPAM(networks, nil, reservations)
				if len(report.Networks) != count || report.Networks[count-1].ReservedCount != 10 {
					b.Fatal("incomplete IPAM report")
				}
			}
		})
	}
}

func BenchmarkIPAMAttribution(b *testing.B) {
	for _, count := range []int{10, 100, 1000, 10000} {
		networks := make([]Network, count)
		hosts := make([]Host, count)
		for i := range networks {
			cidr := fmt.Sprintf("10.%d.%d.0/24", i/256, i%256)
			ip := fmt.Sprintf("10.%d.%d.1", i/256, i%256)
			if i%2 == 0 {
				cidr = fmt.Sprintf("2001:db8:%x::/64", i)
				ip = fmt.Sprintf("2001:db8:%x::1", i)
			}
			networks[i] = Network{ID: int64(i + 1), CIDR: cidr}
			hosts[i] = Host{ID: int64(i + 1), IPs: []string{ip}}
		}
		for _, implementation := range []struct {
			name string
			run  func([]Network, []Host, int) (map[int][]Allocation, []Allocation)
		}{{"indexed", attribute}, {"linear", linearAttribute}} {
			b.Run(fmt.Sprintf("%d/%s", count, implementation.name), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					used, unassigned := implementation.run(networks, hosts, -1)
					if len(used) != count || len(unassigned) != 0 {
						b.Fatal("incomplete attribution")
					}
				}
			})
		}
	}
}
