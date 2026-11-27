package domain

import (
	"strings"
	"testing"
)

func TestDomainValidateDNSLengthLimits(t *testing.T) {
	maxLabel := strings.Repeat("a", 63)
	maxName := strings.Repeat(maxLabel+".", 3) + strings.Repeat("b", 61)
	for _, tc := range []struct {
		name, fqdn string
		valid      bool
	}{
		{"maximum first label", maxLabel + ".example.com", true},
		{"maximum final label", "example." + maxLabel, true},
		{"maximum domain length", maxName, true},
		{"maximum domain with surrounding whitespace", " \t" + maxName + "\n", true},
		{"first label too long", "a" + maxLabel + ".example.com", false},
		{"middle label too long", "host.a" + maxLabel + ".com", false},
		{"final label too long", "example.a" + maxLabel, false},
		{"domain too long", maxName + "b", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (Domain{FQDN: tc.fqdn}).Validate()
			if (err == nil) != tc.valid {
				t.Fatalf("Validate %d-byte name: error=%v, want valid=%t", len(strings.TrimSpace(tc.fqdn)), err, tc.valid)
			}
		})
	}
}
