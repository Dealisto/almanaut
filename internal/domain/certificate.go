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

	ProbeTarget string `yaml:"probe_target" json:"probe_target"` // optional host:port for TLS probing; empty = not probeable

	Probe *CertProbeStatus `yaml:"-" json:"probe,omitempty"` // derived, populated by the detail hook
}

// Validate checks the subject, the expiry date, and (if set) the probe target.
func (c Certificate) Validate() error {
	if strings.TrimSpace(c.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	if err := ValidateCheckAddress(c.ProbeTarget); err != nil {
		return err
	}
	return validateRequiredDate("expiry date", c.ExpiresOn)
}
