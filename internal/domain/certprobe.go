package domain

import (
	"crypto/x509"
	"fmt"
	"net"
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
	SANs       []string // DNS names and canonical IP addresses, for display
	NotAfter   string   // YYYY-MM-DD (probed expiry, for reference)
	Mismatches []string // human-readable reasons; empty => matches inventory
}

// ComputeCertMismatches returns the mismatch reasons for a freshly probed cert,
// comparing against the previous probe (prev may be nil) and the certificate's
// expected subject. IP subjects match only typed IP SANs, never DNS entries.
// Pure — no I/O.
func ComputeCertMismatches(subject string, prev *CertProbeStatus, serial, issuer string, sans []string, ipSANs ...net.IP) []string {
	var out []string
	if prev != nil && prev.Serial != "" && prev.Serial != serial {
		out = append(out, fmt.Sprintf("serial changed (was %s, now %s)", prev.Serial, serial))
	}
	if prev != nil && prev.Issuer != "" && prev.Issuer != issuer {
		out = append(out, fmt.Sprintf("issuer changed (was %q, now %q)", prev.Issuer, issuer))
	}
	if s := strings.TrimSpace(subject); s != "" && !sanCovers(sans, s, ipSANs...) {
		out = append(out, fmt.Sprintf("subject %q not in served SANs", s))
	}
	return out
}

// sanCovers uses the same DNS wildcard, case and IP SAN rules as Go's TLS
// hostname verifier. This checks identity only, not certificate trust or expiry.
func sanCovers(sans []string, name string, ipSANs ...net.IP) bool {
	cert := x509.Certificate{DNSNames: sans, IPAddresses: ipSANs}
	return cert.VerifyHostname(name) == nil
}
