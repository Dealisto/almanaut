package domain

import (
	"net"
	"testing"
)

func TestCertificateIPSubjectsRequireMatchingTypedIPSANs(t *testing.T) {
	for _, tc := range []struct {
		name, subject, address string
		dns                    []string
		matches                bool
	}{
		{"IPv4", "192.0.2.10", "192.0.2.10", nil, true},
		{"IPv6 equivalent spelling", "2001:db8:0:0:0:0:0:10", "2001:db8::10", nil, true},
		{"IPv6 bracketed", "[2001:db8::10]", "2001:db8::10", nil, true},
		{"IPv4 mapped", "::ffff:192.0.2.10", "192.0.2.10", nil, true},
		{"different address", "192.0.2.10", "192.0.2.11", nil, false},
		{"DNS text is not an IP SAN", "192.0.2.10", "", []string{"192.0.2.10"}, false},
		{"DNS wildcard cannot cover an IP", "192.0.2.10", "", []string{"*.0.2.10"}, false},
		{"DNS match alongside IP SAN", "admin.example.com", "192.0.2.10", []string{"admin.example.com"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ips []net.IP
			if tc.address != "" {
				ips = []net.IP{net.ParseIP(tc.address)}
			}
			got := ComputeCertMismatches(tc.subject, nil, "42", "Private CA", tc.dns, ips...)
			if (len(got) == 0) != tc.matches {
				t.Fatalf("mismatches=%v, want matches=%t", got, tc.matches)
			}
		})
	}
}
