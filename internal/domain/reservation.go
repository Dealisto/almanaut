package domain

import (
	"bytes"
	"fmt"
	"net"
	"strings"
)

// Reservation is a named IP range reserved within a network (inclusive of both
// ends). NetworkID is a soft reference (0 = none).
type Reservation struct {
	ID        int64  `yaml:"id" json:"id"`
	NetworkID int64  `yaml:"network_id" json:"network_id"`
	Name      string `yaml:"name" json:"name"`
	StartIP   string `yaml:"start_ip" json:"start_ip"`
	EndIP     string `yaml:"end_ip" json:"end_ip"`
	Notes     string `yaml:"notes" json:"notes"`
}

// Validate requires a name and a valid start/end IP with end >= start.
func (r Reservation) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("name is required")
	}
	start := net.ParseIP(strings.TrimSpace(r.StartIP))
	if start == nil {
		return fmt.Errorf("invalid start IP %q", r.StartIP)
	}
	end := net.ParseIP(strings.TrimSpace(r.EndIP))
	if end == nil {
		return fmt.Errorf("invalid end IP %q", r.EndIP)
	}
	if bytes.Compare(start.To16(), end.To16()) > 0 {
		return fmt.Errorf("end IP must be greater than or equal to start IP")
	}
	return nil
}
