package domain

import "testing"

func TestVLANValidate(t *testing.T) {
	if err := (VLAN{Name: "mgmt", VID: 10}).Validate(); err != nil {
		t.Errorf("valid VLAN rejected: %v", err)
	}
	if err := (VLAN{Name: "", VID: 10}).Validate(); err == nil {
		t.Error("blank name should be rejected")
	}
	if err := (VLAN{Name: "x", VID: 0}).Validate(); err == nil {
		t.Error("vid 0 should be rejected")
	}
	if err := (VLAN{Name: "x", VID: 4095}).Validate(); err == nil {
		t.Error("vid 4095 should be rejected")
	}
}
