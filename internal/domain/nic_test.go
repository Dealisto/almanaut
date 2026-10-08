package domain

import "testing"

func TestNICValidate(t *testing.T) {
	if err := (NIC{HostID: 1, Name: "onboard", Kind: "onboard"}).Validate(); err != nil {
		t.Errorf("valid NIC rejected: %v", err)
	}
	if err := (NIC{HostID: 2, Name: "X540-T2", Kind: "expansion", Model: "Intel X540-T2", Serial: "ABC123"}).Validate(); err != nil {
		t.Errorf("valid expansion NIC rejected: %v", err)
	}
	if err := (NIC{HostID: 1, Name: "  ", Kind: "onboard"}).Validate(); err == nil {
		t.Error("blank name should be rejected")
	}
	if err := (NIC{HostID: 0, Name: "onboard", Kind: "onboard"}).Validate(); err == nil {
		t.Error("missing host should be rejected")
	}
	if err := (NIC{HostID: 1, Name: "onboard", Kind: "pci"}).Validate(); err == nil {
		t.Error("unknown kind should be rejected")
	}
}

func TestEntityTypesIncludeNIC(t *testing.T) {
	if !contains(EntityTypes, "nic") {
		t.Error(`EntityTypes should include "nic"`)
	}
}
