package domain

import (
	"fmt"
	"strings"
)

// Rack is an equipment rack within a Location. LocationID is a soft reference
// (0 = none). UHeight is the rack's capacity in rack units.
type Rack struct {
	ID         int64  `yaml:"id" json:"id"`
	Name       string `yaml:"name" json:"name"`
	LocationID int64  `yaml:"location_id" json:"location_id"`
	UHeight    int    `yaml:"u_height" json:"u_height"`
	Notes      string `yaml:"notes" json:"notes"`
}

// Validate checks required fields and the U-height range.
func (r Rack) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if r.UHeight < 1 || r.UHeight > 60 {
		return fmt.Errorf("u_height must be between 1 and 60")
	}
	return nil
}
