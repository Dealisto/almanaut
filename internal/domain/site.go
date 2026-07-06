package domain

import (
	"fmt"
	"strings"
)

// Site is a physical site (building/campus) in the homelab hierarchy.
type Site struct {
	ID      int64  `yaml:"id" json:"id"`
	Name    string `yaml:"name" json:"name"`
	Address string `yaml:"address" json:"address"`
	Notes   string `yaml:"notes" json:"notes"`
}

// Validate checks required fields.
func (s Site) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}
