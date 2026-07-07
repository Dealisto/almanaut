// Package domain holds the core entities and their validation rules.
package domain

import (
	"fmt"
	"net"
	"strings"
)

// HostTypes is the closed set of allowed Host.Type values.
var HostTypes = []string{"physical", "vm", "lxc", "vps"}

// Host is a machine in the homelab (physical, VM, LXC, or VPS).
type Host struct {
	ID     int64    `yaml:"id" json:"id"`
	Name   string   `yaml:"name" json:"name"`
	Type   string   `yaml:"type" json:"type"`
	OS     string   `yaml:"os" json:"os"`
	CPU    string   `yaml:"cpu" json:"cpu"`
	RAM    string   `yaml:"ram" json:"ram"`
	Disk   string   `yaml:"disk" json:"disk"`
	Status string   `yaml:"status" json:"status"`
	IPs    []string `yaml:"ips" json:"ips"`
	Notes  string   `yaml:"notes" json:"notes"`

	RackID       int64 `yaml:"rack_id" json:"rack_id"`
	RackPosition int   `yaml:"rack_position" json:"rack_position"`
	UHeight      int   `yaml:"u_height" json:"u_height"`
}

// Validate checks required fields and value formats.
func (h Host) Validate() error {
	if strings.TrimSpace(h.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !contains(HostTypes, h.Type) {
		return fmt.Errorf("type %q must be one of %v", h.Type, HostTypes)
	}
	for _, ip := range h.IPs {
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("invalid IP address: %q", ip)
		}
	}
	return validateRackPlacement(h.RackID, h.RackPosition, h.UHeight)
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
