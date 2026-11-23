package domain

import (
	"math"
	"testing"
)

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

func TestHostRackPlacement(t *testing.T) {
	// Unassigned: position/height ignored.
	if err := (Host{Name: "h", Type: "vm"}).Validate(); err != nil {
		t.Errorf("unassigned host rejected: %v", err)
	}
	// Assigned but position 0 -> rejected.
	if err := (Host{Name: "h", Type: "vm", RackID: 1, RackPosition: 0, UHeight: 1}).Validate(); err == nil {
		t.Error("assigned host with position 0 should be rejected")
	}
	// Assigned, valid.
	if err := (Host{Name: "h", Type: "vm", RackID: 1, RackPosition: 3, UHeight: 2}).Validate(); err != nil {
		t.Errorf("valid placed host rejected: %v", err)
	}
}

func TestHardwareRackPlacement(t *testing.T) {
	// Unassigned: position/height ignored.
	if err := (Hardware{Name: "hw"}).Validate(); err != nil {
		t.Errorf("unassigned hardware rejected: %v", err)
	}
	if err := (Hardware{Name: "hw", RackID: 2, RackPosition: 1, UHeight: 0}).Validate(); err == nil {
		t.Error("assigned hardware with u_height 0 should be rejected")
	}
	if err := (Hardware{Name: "hw", RackID: 2, RackPosition: 5, UHeight: 1}).Validate(); err != nil {
		t.Errorf("valid placed hardware rejected: %v", err)
	}
}

func TestRackPlacementBounds(t *testing.T) {
	for _, value := range []int{-1, 0, MaxRackUnits + 1, math.MaxInt} {
		for _, placement := range [][2]int{{value, 1}, {1, value}} {
			host := Host{Name: "host", Type: "physical", RackID: 1, RackPosition: placement[0], UHeight: placement[1]}
			hardware := Hardware{Name: "hardware", RackID: 1, RackPosition: placement[0], UHeight: placement[1]}
			if host.Validate() == nil || hardware.Validate() == nil {
				t.Errorf("invalid placement accepted: %v", placement)
			}
		}
	}
	if err := (Host{Name: "host", Type: "physical", RackID: 1, RackPosition: MaxRackUnits, UHeight: MaxRackUnits}).Validate(); err != nil {
		t.Fatal(err)
	}
}
