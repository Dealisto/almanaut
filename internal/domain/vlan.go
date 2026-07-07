package domain

import (
	"fmt"
	"strings"
)

// VLAN is an 802.1Q virtual LAN. VID is the tag (1..4094).
type VLAN struct {
	ID    int64  `yaml:"id" json:"id"`
	Name  string `yaml:"name" json:"name"`
	VID   int    `yaml:"vid" json:"vid"`
	Notes string `yaml:"notes" json:"notes"`
}

// Validate requires a name and a VLAN id in the 802.1Q range.
func (v VLAN) Validate() error {
	if strings.TrimSpace(v.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if v.VID < 1 || v.VID > 4094 {
		return fmt.Errorf("vid must be between 1 and 4094")
	}
	return nil
}
