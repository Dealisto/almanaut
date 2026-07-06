package domain

import (
	"fmt"
	"strings"
)

// Location is a room/area within a Site. SiteID is a soft reference (0 = none).
type Location struct {
	ID     int64  `yaml:"id" json:"id"`
	Name   string `yaml:"name" json:"name"`
	SiteID int64  `yaml:"site_id" json:"site_id"`
	Notes  string `yaml:"notes" json:"notes"`
}

// Validate checks required fields.
func (l Location) Validate() error {
	if strings.TrimSpace(l.Name) == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
