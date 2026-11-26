package store

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

func TestCompareInventoryValuesPrecision(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want int
	}{
		{"9007199254740992", "9007199254740993", -1},
		{"9223372036854775806", "9223372036854775807", -1},
		{"-9007199254740993", "-9007199254740992", -1},
		{"1.00000000000000000001", "1.00000000000000000002", -1},
		{"0", "1e-1000", -1},
		{"-1e-1000", "0", -1},
		{"1e-1001", "1e-1000", -1},
		{" 9007199254740993 ", "9.007199254740993e15", 0},
		{"0x1p-3", "0.125", 0},
		{"1_000", "1000", 0},
		{"Inf", "+Inf", 0},
		{"-Inf", "-1e300", -1},
		{"NaN", "nan", 0},
		{"10", "text", -1},
		{"Alpha", "beta", -1},
	} {
		t.Run(tt.a+"/"+tt.b, func(t *testing.T) {
			if got := CompareInventoryValues(tt.a, tt.b); got != tt.want {
				t.Fatalf("compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			if got := CompareInventoryValues(tt.b, tt.a); got != -tt.want {
				t.Fatalf("reverse comparison = %d, want %d", got, -tt.want)
			}
		})
	}
}

func TestInventoryIDsSortPreciseNumbers(t *testing.T) {
	db := newTestDB(t)
	// Names intentionally oppose numeric order, so the fallback name ordering
	// exposes values that the collation incorrectly treats as equal.
	if _, err := db.Exec(`INSERT INTO hosts (id, name, type, ram) VALUES
		(1, 'a', 'physical', '9007199254740993'),
		(2, 'z', 'physical', '9007199254740992')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	spec := InventoryQuery{Type: "host", Table: "hosts", Order: "e.name", Fields: []InventoryField{{"RAM", "e.ram"}}}
	for _, desc := range []bool{false, true} {
		want := []int64{2, 1}
		if desc {
			want = []int64{1, 2}
		}
		ids, total, err := InventoryIDs(context.Background(), tx, spec, InventoryFilter{Sort: "RAM", Desc: desc}, 50, 0)
		if err != nil || total != 2 || !reflect.DeepEqual(ids, want) {
			t.Fatalf("descending=%t: ids=%v total=%d err=%v; want %v", desc, ids, total, err, want)
		}
	}
}

func TestCompareInventoryValuesExtremeExponentOrder(t *testing.T) {
	values := []string{
		"1e-2147483649", "0", "1e-1000", "-1e-1000",
		"-1e-2147483649", "1e-9999999999", "0e-2147483649",
		" 1e-2147483649 ", "1", "-1", "Inf", "NaN",
	}
	for _, a := range values {
		for _, b := range values {
			ab := CompareInventoryValues(a, b)
			if ba := CompareInventoryValues(b, a); ab != -ba {
				t.Fatalf("asymmetric comparison of %q and %q: %d, %d", a, b, ab, ba)
			}
			for _, c := range values {
				bc := CompareInventoryValues(b, c)
				ac := CompareInventoryValues(a, c)
				if ab <= 0 && bc <= 0 && ac > 0 {
					t.Fatalf("non-transitive order: %q <= %q <= %q, but first > last", a, b, c)
				}
			}
		}
	}
}
