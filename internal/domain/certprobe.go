package domain

import (
	"fmt"
	"strings"
	"time"
)

// CertProbeStatus is the most recent TLS probe result for a certificate. It is
// derived runtime state (not user-edited); nil means "never probed".
type CertProbeStatus struct {
	ProbedAt   time.Time
	Success    bool
	LastError  string // set when Success is false
	Serial     string
	Issuer     string
	SANs       []string
	NotAfter   string   // YYYY-MM-DD (probed expiry, for reference)
	Mismatches []string // human-readable reasons; empty => matches inventory
}

// ComputeCertMismatches returns the mismatch reasons for a freshly probed cert,
// comparing against the previous probe (prev may be nil) and the certificate's
// expected subject. Pure — no I/O.
func ComputeCertMismatches(subject string, prev *CertProbeStatus, serial, issuer string, sans []string) []string {
	var out []string
	if prev != nil && prev.Serial != "" && prev.Serial != serial {
		out = append(out, fmt.Sprintf("serial changed (was %s, now %s)", prev.Serial, serial))
	}
	if prev != nil && prev.Issuer != "" && prev.Issuer != issuer {
		out = append(out, fmt.Sprintf("issuer changed (was %q, now %q)", prev.Issuer, issuer))
	}
	if s := strings.TrimSpace(subject); s != "" && !sanCovers(sans, s) {
		out = append(out, fmt.Sprintf("subject %q not in served SANs", s))
	}
	return out
}

// sanCovers reports whether name is matched by any SAN, case-insensitively,
// honouring a single leading-wildcard label (*.example.com matches a.example.com
// but not example.com or a.b.example.com).
func sanCovers(sans []string, name string) bool {
	name = strings.ToLower(name)
	for _, san := range sans {
		san = strings.ToLower(strings.TrimSpace(san))
		if san == name {
			return true
		}
		if strings.HasPrefix(san, "*.") {
			suffix := san[1:] // ".example.com"
			if strings.HasSuffix(name, suffix) {
				host := name[:len(name)-len(suffix)]
				if host != "" && !strings.Contains(host, ".") {
					return true
				}
			}
		}
	}
	return false
}
