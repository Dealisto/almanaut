package domain

import (
	"fmt"
	"strings"
)

// DateLayout is the canonical date format for date fields (YYYY-MM-DD).
const DateLayout = "2006-01-02"

// Certificate is a TLS certificate tracked in the homelab.
type Certificate struct {
	ID        int64  `yaml:"id"`
	Subject   string `yaml:"subject"` // domain(s) the cert covers
	Issuer    string `yaml:"issuer"`
	ExpiresOn string `yaml:"expires_on"` // YYYY-MM-DD
	AutoRenew bool   `yaml:"auto_renew"`
	Notes     string `yaml:"notes"`
}

// Validate checks the subject and the expiry date.
func (c Certificate) Validate() error {
	if strings.TrimSpace(c.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	return validateRequiredDate("expiry date", c.ExpiresOn)
}
