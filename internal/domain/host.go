// Package domain holds the core entities and their validation rules.
package domain

import (
	"fmt"
	"net"
	"strings"
)

// HostTypes contains the built-in type names used by discovery and the agent.
// Additional types and display labels are stored in the host type catalog.
var HostTypes = []string{"physical", "vm", "lxc", "vps"}

// Host is a machine in the homelab (physical, VM, LXC, or VPS).
type Host struct {
	// IPAMEntity is set only on temporary allocation projections, never on inventory.
	IPAMEntity *EntityRef `json:"-" yaml:"-"`
	ID         int64      `yaml:"id" json:"id"`
	Name       string     `yaml:"name" json:"name"`
	Type       string     `yaml:"type" json:"type"`
	TypeLabel  string     `yaml:"-" json:"-"` // derived display label; Type stays stable
	OS         string     `yaml:"os" json:"os"`
	CPU        string     `yaml:"cpu" json:"cpu"`
	RAM        string     `yaml:"ram" json:"ram"`
	Disk       string     `yaml:"disk" json:"disk"`
	Status     string     `yaml:"status" json:"status"`
	IPs        []string   `yaml:"ips" json:"ips"`
	Notes      string     `yaml:"notes" json:"notes"`
	// AgentProtectedFields keeps selected inventory values under manual control.
	// On update, nil preserves the stored policy; an explicit empty list clears it.
	AgentProtectedFields []string `yaml:"agent_protected_fields" json:"agent_protected_fields"`

	CheckAddress string          `yaml:"check_address" json:"check_address"` // optional host:port for TCP liveness checks; empty = not monitored
	Liveness     *LivenessStatus `yaml:"-" json:"liveness,omitempty"`        // derived, populated by the repo join

	RackID       int64 `yaml:"rack_id" json:"rack_id"`
	RackPosition int   `yaml:"rack_position" json:"rack_position"`
	UHeight      int   `yaml:"u_height" json:"u_height"`
}

// Validate checks required fields and value formats.
func (h Host) Validate() error {
	if strings.TrimSpace(h.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !hostTypeNameRE.MatchString(h.Type) {
		return fmt.Errorf("invalid host type name %q", h.Type)
	}
	// Catalog membership is checked by the repository in the write transaction.
	if err := h.ValidateAgentProtection(); err != nil {
		return err
	}
	for _, ip := range h.IPs {
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("invalid IP address: %q", ip)
		}
	}
	if err := ValidateCheckAddress(h.CheckAddress); err != nil {
		return err
	}
	return validateRackPlacement(h.RackID, h.RackPosition, h.UHeight)
}

// AgentProtects reports whether reports must preserve this inventory field.
func (h Host) AgentProtects(field string) bool {
	return contains(h.AgentProtectedFields, field)
}

// ValidateAgentProtection rejects unknown and repeated protection names.
func (h Host) ValidateAgentProtection() error {
	seen := make(map[string]bool, len(h.AgentProtectedFields))
	for _, field := range h.AgentProtectedFields {
		switch field {
		case "ips", "os", "cpu", "ram", "disk":
		default:
			return fmt.Errorf("invalid agent-protected field %q: use ips, os, cpu, ram, or disk", field)
		}
		if seen[field] {
			return fmt.Errorf("duplicate agent-protected field %q", field)
		}
		seen[field] = true
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
