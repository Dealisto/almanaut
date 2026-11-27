package store

import (
	"fmt"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

func TestInventoryUnderflowDecimalOrder(t *testing.T) {
	zeros := strings.Repeat("0", 10000)
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1." + zeros + "1e-1000", "1." + zeros + "2e-1000", -1},
		{"-1." + zeros + "1e-1000", "-1." + zeros + "2e-1000", 1},
		{"-1e-1000", "1e-1000", -1},
		{"0", "1e-1000", -1},
		{"-0.000e-1000", "1e-1000", -1},
		{"-1e-1000", "-0", -1},
		{"+0", "-0.000e-1000", 0},
		{"0." + zeros + "1", "0." + zeros + "2", -1},
		{"1_000e-1003", "1e-1000", 0},
		{" 1.000e-1000 ", "10e-1001", 0},
	} {
		if got := CompareInventoryValues(tc.a, tc.b); got != tc.want {
			t.Errorf("order=%d want=%d (lengths=%d/%d)", got, tc.want, len(tc.a), len(tc.b))
		}
		if got := CompareInventoryValues(tc.b, tc.a); got != -tc.want {
			t.Errorf("reverse order=%d want=%d", got, -tc.want)
		}
	}
}

// oldZeroTieOrder is the exact previous float64-zero tie path. Keeping this
// oracle independent of the new decimal parser checks the historical ordering
// subgroup for exponents that big.Float cannot represent, not just real-number
// order. All inputs in the differential test first verify that float64 is zero.
func oldZeroTieOrder(a, b string) int {
	if a == b {
		return 0
	}
	precision := uint(64 + 4*(len(a)+len(b)))
	aa, _, aerr := big.ParseFloat(strings.TrimSpace(a), 0, precision, big.ToNearestEven)
	bb, _, berr := big.ParseFloat(strings.TrimSpace(b), 0, precision, big.ToNearestEven)
	if aerr == nil && berr == nil {
		return aa.Cmp(bb)
	}
	if aerr == nil {
		return -1
	}
	if berr == nil {
		return 1
	}
	return strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b))
}

func TestInventoryUnderflowMatchesPreviousOrder(t *testing.T) {
	// 2^-1075 has an exact finite decimal spelling. Comparing it to its hex
	// form exercises equality between the direct decimal and fallback domains.
	denominator := new(big.Int).Lsh(big.NewInt(1), 1075)
	tiny := new(big.Rat).SetFrac(big.NewInt(1), denominator).FloatString(1075)
	values := []string{
		"0", "-0", "+0", "-0.000", " 0.000e-1000 ",
		"0e-9999999999", "-0e-9999999999", "0e-999999999999999999999999",
		"1e-1000", "-1e-1000", "1_000e-1003", "1e-1001", "-1e-1001",
		"1e-67108863", "1e-67108864", "1e-67108865", "-1e-67108864", "-1e-67108865",
		"1e-2147483649", "-1e-2147483649", " 1e-2147483649 ", "1e-9999999999",
		"1e-9223372036854775808", "0.01e-9223372036854775808",
		"0x0p0", "-0x0p-9999", "0x1p-1075", "-0x1p-1075", "0x1p-2147483649",
		tiny, "-" + tiny,
	}
	rng := rand.New(rand.NewSource(2394))
	for range 20 {
		var digits strings.Builder
		for range 1 + rng.Intn(80) {
			digits.WriteByte(byte('0' + rng.Intn(10)))
		}
		sign := ""
		if rng.Intn(2) == 0 {
			sign = "-"
		}
		values = append(values, fmt.Sprintf("%s%d.%se-%d", sign, rng.Intn(10), digits.String(), 400+rng.Intn(800)))
	}
	for _, value := range values {
		if number, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err != nil || number != 0 {
			t.Fatalf("oracle requires zero float64, got %g, %v for %q", number, err, value)
		}
	}
	comparisons := make([][]int, len(values))
	for i, a := range values {
		comparisons[i] = make([]int, len(values))
		for j, b := range values {
			got, want := CompareInventoryValues(a, b), oldZeroTieOrder(a, b)
			if got != want {
				t.Fatalf("order=%d want old=%d for %q and %q", got, want, a, b)
			}
			comparisons[i][j] = got
		}
	}
	for i := range values {
		for j := range values {
			if comparisons[i][j] != -comparisons[j][i] {
				t.Fatalf("asymmetric order for indices %d/%d", i, j)
			}
			for k := range values {
				if comparisons[i][j] <= 0 && comparisons[j][k] <= 0 && comparisons[i][k] > 0 {
					t.Fatalf("non-transitive order for indices %d/%d/%d", i, j, k)
				}
			}
		}
	}
}

func BenchmarkInventoryUnderflowDecimalOrder(b *testing.B) {
	zeros := strings.Repeat("0", 200000)
	a, other := "1."+zeros+"1e-1000", "1."+zeros+"2e-1000"
	b.ReportAllocs()
	for b.Loop() {
		if CompareInventoryValues(a, other) != -1 {
			b.Fatal("incorrect numeric order")
		}
	}
}
