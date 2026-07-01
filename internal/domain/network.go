package domain

import (
	"fmt"
	"net"
	"strings"
)

// Network is an IP subnet / VLAN in the homelab.
type Network struct {
	ID      int64  `yaml:"id" json:"id"`
	Name    string `yaml:"name" json:"name"`
	CIDR    string `yaml:"cidr" json:"cidr"`
	VLAN    string `yaml:"vlan" json:"vlan"` // free text (may be blank)
	Gateway string `yaml:"gateway" json:"gateway"`
	Notes   string `yaml:"notes" json:"notes"`
}

// Validate checks required fields and address formats.
func (n Network) Validate() error {
	if strings.TrimSpace(n.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(n.CIDR) == "" {
		return fmt.Errorf("CIDR is required")
	}
	if _, _, err := net.ParseCIDR(n.CIDR); err != nil {
		return fmt.Errorf("invalid CIDR %q", n.CIDR)
	}
	if g := strings.TrimSpace(n.Gateway); g != "" && net.ParseIP(g) == nil {
		return fmt.Errorf("invalid gateway IP %q", g)
	}
	return nil
}
