package domain

import "testing"

func TestReservationAddressFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, start, end string
		valid            bool
	}{
		{"IPv4", "10.0.0.1", "10.0.0.2", true},
		{"IPv6", "2001:db8::1", "2001:db8::2", true},
		{"mapped IPv4", "10.0.0.1", "::ffff:10.0.0.2", true},
		{"IPv4 to IPv6", "10.0.0.1", "2001:db8::1", false},
		{"IPv6 to IPv4", "::1", "10.0.0.1", false},
		{"mapped IPv4 to IPv6", "::ffff:10.0.0.1", "2001:db8::1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (Reservation{Name: "range", StartIP: tc.start, EndIP: tc.end}).Validate()
			if (err == nil) != tc.valid {
				t.Fatalf("Validate() = %v, want valid=%v", err, tc.valid)
			}
		})
	}
}

func TestReservationValidate(t *testing.T) {
	if err := (Reservation{Name: "dhcp", StartIP: "10.0.0.10", EndIP: "10.0.0.50"}).Validate(); err != nil {
		t.Errorf("valid reservation rejected: %v", err)
	}
	if err := (Reservation{Name: "one", StartIP: "10.0.0.5", EndIP: "10.0.0.5"}).Validate(); err != nil {
		t.Errorf("single-address reservation should be valid: %v", err)
	}
	if err := (Reservation{Name: "", StartIP: "10.0.0.1", EndIP: "10.0.0.2"}).Validate(); err == nil {
		t.Error("blank name should be rejected")
	}
	if err := (Reservation{Name: "x", StartIP: "nope", EndIP: "10.0.0.2"}).Validate(); err == nil {
		t.Error("invalid start IP should be rejected")
	}
	if err := (Reservation{Name: "x", StartIP: "10.0.0.50", EndIP: "10.0.0.10"}).Validate(); err == nil {
		t.Error("end < start should be rejected")
	}
}
