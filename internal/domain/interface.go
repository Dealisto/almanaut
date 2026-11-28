package domain

import (
	"fmt"
	"net"
	"strings"
)

var InterfaceKinds = []string{"physical", "virtual", "bridge", "bond", "vlan", "loopback"}

// NetworkInterface is a logical interface. An optional port links it to cabling.
type NetworkInterface struct {
	ID        int64  `json:"id" yaml:"id"`
	OwnerType string `json:"owner_type" yaml:"owner_type"`
	OwnerID   int64  `json:"owner_id" yaml:"owner_id"`
	Name      string `json:"name" yaml:"name"`
	Kind      string `json:"kind" yaml:"kind"`
	PortID    int64  `json:"port_id" yaml:"port_id"`
	VLANID    int64  `json:"vlan_id" yaml:"vlan_id"`
	MAC       string `json:"mac" yaml:"mac"`
	MTU       int    `json:"mtu" yaml:"mtu"`
	Enabled   bool   `json:"enabled" yaml:"enabled"`
	Notes     string `json:"notes" yaml:"notes"`
}

func (v NetworkInterface) Validate() error {
	if strings.TrimSpace(v.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !contains(PortOwnerTypes, v.OwnerType) || v.OwnerID <= 0 {
		return fmt.Errorf("an interface must belong to a host or hardware item")
	}
	if !contains(InterfaceKinds, v.Kind) {
		return fmt.Errorf("invalid interface kind %q", v.Kind)
	}
	if v.PortID < 0 || v.VLANID < 0 {
		return fmt.Errorf("invalid port or VLAN reference")
	}
	if v.MTU < 0 || v.MTU > 65535 {
		return fmt.Errorf("MTU must be between 0 (unknown) and 65535")
	}
	if v.MAC != "" {
		if _, err := net.ParseMAC(v.MAC); err != nil {
			return fmt.Errorf("invalid MAC address %q", v.MAC)
		}
	}
	return nil
}
