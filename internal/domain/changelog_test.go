package domain

import "testing"

func TestDiffReportsChangedFieldsOnly(t *testing.T) {
	old := Host{ID: 1, Name: "nas", Status: "running", RAM: "8G"}
	new := Host{ID: 1, Name: "nas", Status: "down", RAM: "16G"}
	changes, err := Diff(old, new)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("want 2 changes, got %d: %+v", len(changes), changes)
	}
	// sorted by field: "ram" before "status"
	if changes[0].Field != "ram" || changes[0].Old != "8G" || changes[0].New != "16G" {
		t.Errorf("ram change wrong: %+v", changes[0])
	}
	if changes[1].Field != "status" || changes[1].Old != "running" || changes[1].New != "down" {
		t.Errorf("status change wrong: %+v", changes[1])
	}
}

func TestDiffIgnoresIDAndUnchanged(t *testing.T) {
	h := Host{ID: 1, Name: "nas", Status: "running"}
	changes, err := Diff(h, Host{ID: 2, Name: "nas", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("id-only difference should produce no changes, got %+v", changes)
	}
}

func TestDiffRendersSlices(t *testing.T) {
	changes, err := Diff(
		Host{ID: 1, IPs: []string{"10.0.0.1"}},
		Host{ID: 1, IPs: []string{"10.0.0.1", "10.0.0.2"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Field != "ips" {
		t.Fatalf("want one ips change, got %+v", changes)
	}
	if changes[0].Old != "10.0.0.1" || changes[0].New != "10.0.0.1, 10.0.0.2" {
		t.Errorf("ips rendered wrong: %+v", changes[0])
	}
}

func TestDiffRendersLargeIntegerWithoutScientificNotation(t *testing.T) {
	// Numeric fields round-trip through JSON as float64; a large integer must
	// render as a plain integer, not "1.5e+06" (regression from fmt "%v").
	changes, err := Diff(Host{ID: 1}, Host{ID: 1, RackPosition: 1500000})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Field != "rack_position" {
		t.Fatalf("want one rack_position change, got %+v", changes)
	}
	if changes[0].New != "1500000" {
		t.Errorf("large integer rendered wrong: got %q, want %q", changes[0].New, "1500000")
	}
}

func TestRenderValueFormatsNumbers(t *testing.T) {
	cases := map[any]string{
		float64(1500000):                "1500000", // no scientific notation
		float64(0):                      "0",
		float64(2.5):                    "2.5", // fractional preserved
		[]any{float64(1000000), "eth0"}: "1000000, eth0",
	}
	for in, want := range cases {
		if got := renderValue(in); got != want {
			t.Errorf("renderValue(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestDiffCreateFromZero(t *testing.T) {
	changes, err := Diff(Host{}, Host{ID: 1, Name: "nas", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	// name + status set, id skipped, everything else empty→empty (unchanged)
	got := map[string]FieldChange{}
	for _, c := range changes {
		got[c.Field] = c
	}
	if got["name"].New != "nas" || got["name"].Old != "" {
		t.Errorf("name: %+v", got["name"])
	}
	if got["status"].New != "running" {
		t.Errorf("status: %+v", got["status"])
	}
}
