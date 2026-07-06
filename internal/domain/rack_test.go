package domain

import "testing"

func TestSiteValidate(t *testing.T) {
	if err := (Site{Name: "hq"}).Validate(); err != nil {
		t.Errorf("valid site rejected: %v", err)
	}
	if err := (Site{Name: "  "}).Validate(); err == nil {
		t.Error("blank site name should be rejected")
	}
}

func TestLocationValidate(t *testing.T) {
	if err := (Location{Name: "room1", SiteID: 3}).Validate(); err != nil {
		t.Errorf("valid location rejected: %v", err)
	}
	if err := (Location{Name: ""}).Validate(); err == nil {
		t.Error("blank location name should be rejected")
	}
}

func TestRackValidate(t *testing.T) {
	if err := (Rack{Name: "r1", UHeight: 42}).Validate(); err != nil {
		t.Errorf("valid rack rejected: %v", err)
	}
	if err := (Rack{Name: "r1", UHeight: 0}).Validate(); err == nil {
		t.Error("u_height 0 should be rejected")
	}
	if err := (Rack{Name: "r1", UHeight: 61}).Validate(); err == nil {
		t.Error("u_height 61 should be rejected")
	}
	if err := (Rack{Name: "", UHeight: 42}).Validate(); err == nil {
		t.Error("blank rack name should be rejected")
	}
}
