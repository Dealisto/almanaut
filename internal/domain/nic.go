package domain

import (
	"fmt"
	"strings"
)

// NICKinds is the closed set of NIC kinds: built into the machine or an
// expansion card.
var NICKinds = []string{"onboard", "expansion"}

// NIC is a network interface card on a host, onboard or an expansion card.
// Ports reference a NIC via their nic_id soft reference; a NIC groups them
// without owning them. HostID is a soft reference (0 = none is invalid).
type NIC struct {
	ID     int64  `yaml:"id" json:"id"`
	HostID int64  `yaml:"host_id" json:"host_id"`
	Name   string `yaml:"name" json:"name"`
	Kind   string `yaml:"kind" json:"kind"`
	Model  string `yaml:"model" json:"model"`
	Serial string `yaml:"serial" json:"serial"`
	Notes  string `yaml:"notes" json:"notes"`

	// HostName is derived by the repo for display; never persisted, diffed,
	// or imported (both tags "-").
	HostName string `yaml:"-" json:"-"`
}

// Validate requires a name, a host reference, and a known kind.
func (n NIC) Validate() error {
	if strings.TrimSpace(n.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if n.HostID <= 0 {
		return fmt.Errorf("a NIC must belong to a host")
	}
	if !contains(NICKinds, n.Kind) {
		return fmt.Errorf("invalid kind %q", n.Kind)
	}
	return nil
}
