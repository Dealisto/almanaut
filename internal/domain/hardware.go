package domain

import (
	"fmt"
	"strings"
)

// Hardware is a piece of physical equipment in the homelab: UPS, switch, disk,
// NAS, server, etc. Warranty/purchase dates are optional YYYY-MM-DD strings.
type Hardware struct {
	ID           int64  `yaml:"id"`
	Name         string `yaml:"name"`
	Kind         string `yaml:"kind"` // free text: ups, switch, disk, nas…
	Manufacturer string `yaml:"manufacturer"`
	Model        string `yaml:"model"`
	Serial       string `yaml:"serial"`
	Location     string `yaml:"location"`
	PurchaseDate string `yaml:"purchase_date"` // optional YYYY-MM-DD
	WarrantyEnd  string `yaml:"warranty_end"`  // optional YYYY-MM-DD
	Status       string `yaml:"status"`        // free text: active/spare/retired
	Notes        string `yaml:"notes"`
}

// Validate checks the name and (if present) the purchase/warranty dates.
func (h Hardware) Validate() error {
	if strings.TrimSpace(h.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if err := validateOptionalDate("purchase date", h.PurchaseDate); err != nil {
		return err
	}
	return validateOptionalDate("warranty end", h.WarrantyEnd)
}
