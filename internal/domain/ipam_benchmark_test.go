package domain

import (
	"fmt"
	"testing"
)

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
