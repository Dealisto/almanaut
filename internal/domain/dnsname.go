package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// fqdnPattern matches a dotted, multi-label hostname with a 2+ letter TLD.
var fqdnPattern = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)

// Domain is a DNS name pointing at something in the homelab.
type Domain struct {
	ID       int64  `yaml:"id"`
	FQDN     string `yaml:"fqdn"`
	Provider string `yaml:"provider"` // DNS provider
	Notes    string `yaml:"notes"`
}

// Validate checks that the FQDN is present and well-formed.
func (d Domain) Validate() error {
	fqdn := strings.TrimSpace(d.FQDN)
	if fqdn == "" {
		return fmt.Errorf("FQDN is required")
	}
	if !fqdnPattern.MatchString(fqdn) {
		return fmt.Errorf("invalid FQDN %q", d.FQDN)
	}
	return nil
}
