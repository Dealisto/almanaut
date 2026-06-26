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
	ID        int64
	Subject   string // domain(s) the cert covers
	Issuer    string
	ExpiresOn string // YYYY-MM-DD
	AutoRenew bool
	Notes     string
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
