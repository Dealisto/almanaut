package domain

import (
	"fmt"
	"strings"
)

// DateLayout is the canonical date format for date fields (YYYY-MM-DD).
const DateLayout = "2006-01-02"

// Certificate is a TLS certificate tracked in the homelab.
type Certificate struct {
	ID        int64  `yaml:"id" json:"id"`
	Subject   string `yaml:"subject" json:"subject"` // domain(s) the cert covers
	Issuer    string `yaml:"issuer" json:"issuer"`
	ExpiresOn string `yaml:"expires_on" json:"expires_on"` // YYYY-MM-DD
	AutoRenew bool   `yaml:"auto_renew" json:"auto_renew"`
	Notes     string `yaml:"notes" json:"notes"`
}

// Validate checks the subject and the expiry date.
func (c Certificate) Validate() error {
	if strings.TrimSpace(c.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	return validateRequiredDate("expiry date", c.ExpiresOn)
}
