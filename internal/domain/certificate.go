package domain

import (
	"fmt"
	"strings"
	"time"
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
	if strings.TrimSpace(c.ExpiresOn) == "" {
		return fmt.Errorf("expiry date is required")
	}
	if _, err := time.Parse(DateLayout, c.ExpiresOn); err != nil {
		return fmt.Errorf("expiry date must be YYYY-MM-DD, got %q", c.ExpiresOn)
	}
	return nil
}
