package domain

import "testing"

func TestPortValidate(t *testing.T) {
	if err := (Port{OwnerType: "hardware", OwnerID: 1, Name: "Port 1"}).Validate(); err != nil {
		t.Errorf("valid switch port rejected: %v", err)
	}
	if err := (Port{OwnerType: "host", OwnerID: 2, NICID: 3, Name: "eth0", MAC: "aa:bb:cc:dd:ee:ff", MgmtOnly: true, PeerPortID: 4}).Validate(); err != nil {
		t.Errorf("valid host port rejected: %v", err)
	}
	if err := (Port{OwnerType: "host", OwnerID: 1, Name: " "}).Validate(); err == nil {
		t.Error("blank name should be rejected")
	}
	if err := (Port{OwnerType: "service", OwnerID: 1, Name: "eth0"}).Validate(); err == nil {
		t.Error("owner type outside host/hardware should be rejected")
	}
	if err := (Port{OwnerType: "host", OwnerID: 0, Name: "eth0"}).Validate(); err == nil {
		t.Error("missing owner should be rejected")
	}
	if err := (Port{OwnerType: "host", OwnerID: 1, NICID: -1, Name: "eth0"}).Validate(); err == nil {
		t.Error("negative nic reference should be rejected")
	}
	if err := (Port{OwnerType: "host", OwnerID: 1, Name: "eth0", PeerPortID: -1}).Validate(); err == nil {
		t.Error("negative peer reference should be rejected")
	}
	if err := (Port{ID: 5, OwnerType: "host", OwnerID: 1, Name: "eth0", PeerPortID: 5}).Validate(); err == nil {
		t.Error("self-connection should be rejected")
	}
	if err := (Port{OwnerType: "host", OwnerID: 1, Name: "eth0", MAC: "not-a-mac"}).Validate(); err == nil {
		t.Error("invalid MAC should be rejected")
	}
	if err := (Port{OwnerType: "host", OwnerID: 1, Name: "eth0", MAC: ""}).Validate(); err != nil {
		t.Errorf("empty MAC should be allowed: %v", err)
	}
}

func TestEntityTypesIncludePort(t *testing.T) {
	if !contains(EntityTypes, "port") {
		t.Error(`EntityTypes should include "port"`)
	}
}
