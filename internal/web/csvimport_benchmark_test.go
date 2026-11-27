package web

import (
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func BenchmarkCSVValidationMemory(b *testing.B) {
	for _, tc := range []struct{ name, document string }{
		{"unknown-header-wide-rows", "unknown\n" + strings.Repeat(strings.Repeat(",", 127)+"\n", 8000)},
		{"invalid-first-row", "name,type\n,vm\n" + strings.Repeat("node,vm\n", 20000)},
		{"invalid-last-row", "name,type\n" + strings.Repeat("node,vm\n", 20000) + ",vm\n"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			rs := resource[domain.Host]{newItem: domain.Host{Type: "physical"}, parse: parseHost}
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.document)))
			for b.Loop() {
				created, updated, rowErrs, err := rs.importCSV(handlerDeps{}, strings.NewReader(tc.document), "tester")
				if err != nil || created != 0 || updated != 0 || len(rowErrs) != 1 {
					b.Fatalf("invalid import changed outcome: created=%d updated=%d rows=%v error=%v", created, updated, rowErrs, err)
				}
			}
		})
	}
}
