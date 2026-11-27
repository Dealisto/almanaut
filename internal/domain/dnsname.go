package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// fqdnPattern matches a dotted, multi-label hostname with a 2+ letter TLD.
// Each label is limited to 63 bytes, including the final label.
var fqdnPattern = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,63}$`)

// Domain is a DNS name pointing at something in the homelab.
type Domain struct {
	ID       int64  `yaml:"id" json:"id"`
	FQDN     string `yaml:"fqdn" json:"fqdn"`
	Provider string `yaml:"provider" json:"provider"` // DNS provider
	Notes    string `yaml:"notes" json:"notes"`
}

// Validate checks that the FQDN is present and well-formed.
func (d Domain) Validate() error {
	fqdn := strings.TrimSpace(d.FQDN)
	if fqdn == "" {
		return fmt.Errorf("FQDN is required")
	}
	// DNS wire format adds the first label length and the root terminator
	// to this dotted spelling: 253 text bytes occupy the 255-byte limit.
	if len(fqdn) > 253 || !fqdnPattern.MatchString(fqdn) {
		return fmt.Errorf("invalid FQDN %q", d.FQDN)
	}
	return nil
}
