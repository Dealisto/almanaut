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

// validateRackPlacement checks an occupant's rack placement. Placement is only
// meaningful when the occupant is assigned to a rack (rackID != 0); an
// unassigned occupant (rackID 0) is unconstrained.
func validateRackPlacement(rackID int64, position, uHeight int) error {
	if rackID == 0 {
		return nil
	}
	if position < 1 {
		return fmt.Errorf("rack position must be at least 1 when assigned to a rack")
	}
	if uHeight < 1 {
		return fmt.Errorf("u_height must be at least 1 when assigned to a rack")
	}
	return nil
}
