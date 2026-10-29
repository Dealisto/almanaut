package domain

import (
	"fmt"
	"net"
	"strings"
)

// PortOwnerTypes is the closed set of entity types that can own a Port.
var PortOwnerTypes = []string{"host", "hardware"}

// Port is a physical network port on a host (a NIC port) or on a hardware
// item (a switch/router port). The owner is polymorphic (OwnerType + OwnerID).
// NICID optionally attributes a host port to a NIC, and PeerPortID names the
// port at the other end of the cable — both soft references (0 = none). The
// link is stored on both ports: each one's PeerPortID names the other.
type Port struct {
	ID         int64  `yaml:"id" json:"id"`
	OwnerType  string `yaml:"owner_type" json:"owner_type"`
	OwnerID    int64  `yaml:"owner_id" json:"owner_id"`
	NICID      int64  `yaml:"nic_id" json:"nic_id"`
	Name       string `yaml:"name" json:"name"`
	MAC        string `yaml:"mac" json:"mac"`
	MgmtOnly   bool   `yaml:"mgmt_only" json:"mgmt_only"`
	PeerPortID int64  `yaml:"peer_port_id" json:"peer_port_id"`
	Notes      string `yaml:"notes" json:"notes"`

	// Derived display fields, populated by the repo; never persisted, diffed,
	// or imported (both tags "-").
	OwnerName string `yaml:"-" json:"-"`
	NICName   string `yaml:"-" json:"-"`
	PeerLabel string `yaml:"-" json:"-"`
}

// Validate requires a name and an owner, and checks the soft references and
// the MAC format (empty MAC is allowed).
func (p Port) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !contains(PortOwnerTypes, p.OwnerType) {
		return fmt.Errorf("invalid owner type %q", p.OwnerType)
	}
	if p.OwnerID <= 0 {
		return fmt.Errorf("a port must belong to a host or hardware item")
	}
	if p.NICID < 0 {
		return fmt.Errorf("invalid NIC reference")
	}
	if p.NICID != 0 && p.OwnerType != "host" {
		return fmt.Errorf("only a host's ports can belong to a NIC")
	}
	if p.PeerPortID < 0 {
		return fmt.Errorf("invalid peer port reference")
	}
	if p.ID != 0 && p.PeerPortID == p.ID {
		return fmt.Errorf("a port cannot be connected to itself")
	}
	if mac := strings.TrimSpace(p.MAC); mac != "" {
		if _, err := net.ParseMAC(mac); err != nil {
			return fmt.Errorf("invalid MAC address %q", p.MAC)
		}
	}
	return nil
}
