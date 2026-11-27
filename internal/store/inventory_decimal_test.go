package store

import (
	"strings"
	"testing"
)

func TestInventoryLongDecimalOrder(t *testing.T) {
	zeros := strings.Repeat("0", 100000)
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1." + zeros + "1", "1." + zeros + "2", -1},
		{"-1." + zeros + "1", "-1." + zeros + "2", 1},
		{"1." + zeros, "10e-1", 0},
		{"0.1" + zeros + "1e1", "1", 1},
		{"1_000.000_000_000_000_01", "1000.00000000000002", -1},
		{"0.99999999999999999", "1", -1},
		{"9.99999999999999999", "10", -1},
		{"-9.99999999999999999", "-10", 1},
		{"9.9999999999999999e99", "1e100", -1},
		{"4.9406564584124654e-324", "4.9406564584124655e-324", -1},
		{"-4.9406564584124654e-324", "-4.9406564584124655e-324", 1},
		{"+1.00000000000000001", "0x1p0", 1},
	} {
		if got := CompareInventoryValues(tc.a, tc.b); got != tc.want {
			t.Errorf("comparison = %d, want %d (input lengths %d/%d)", got, tc.want, len(tc.a), len(tc.b))
		}
		if got := CompareInventoryValues(tc.b, tc.a); got != -tc.want {
			t.Errorf("reverse comparison = %d, want %d", got, -tc.want)
		}
	}
}

func BenchmarkInventoryLongDecimalOrder(b *testing.B) {
	zeros := strings.Repeat("0", 1000000)
	a, other := "1."+zeros+"1", "1."+zeros+"2"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if CompareInventoryValues(a, other) != -1 {
			b.Fatal("incorrect numeric order")
		}
	}
}
