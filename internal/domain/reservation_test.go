package domain

import "testing"

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
