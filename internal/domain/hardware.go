package domain

import (
	"fmt"
	"strings"
)

// Hardware is a piece of physical equipment in the homelab: UPS, switch, disk,
// NAS, server, etc. Warranty/purchase dates are optional YYYY-MM-DD strings.
type Hardware struct {
	ID           int64  `yaml:"id" json:"id"`
	Name         string `yaml:"name" json:"name"`
	Kind         string `yaml:"kind" json:"kind"` // free text: ups, switch, disk, nas…
	Manufacturer string `yaml:"manufacturer" json:"manufacturer"`
	Model        string `yaml:"model" json:"model"`
	Serial       string `yaml:"serial" json:"serial"`
	Location     string `yaml:"location" json:"location"`
	PurchaseDate string `yaml:"purchase_date" json:"purchase_date"` // optional YYYY-MM-DD
	WarrantyEnd  string `yaml:"warranty_end" json:"warranty_end"`   // optional YYYY-MM-DD
	Status       string `yaml:"status" json:"status"`               // free text: active/spare/retired
	Notes        string `yaml:"notes" json:"notes"`
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
