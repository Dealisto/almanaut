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
	ID     int64
	Name   string
	Type   string
	OS     string
	CPU    string
	RAM    string
	Disk   string
	Status string
	IPs    []string
	Notes  string
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
