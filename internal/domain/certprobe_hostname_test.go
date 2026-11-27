package domain

import "testing"

func TestCertificateDNSSubjectsUseTLSHostnameRules(t *testing.T) {
	for _, tc := range []struct {
		name, subject, san string
		matches            bool
	}{
		{"absolute DNS name", "example.com.", "example.com", true},
		{"absolute wildcard name", "APP.Example.com.", "*.example.com", true},
		{"whitespace in served SAN", "example.com", " example.com ", false},
		{"wildcard over invalid label", "a/b.example.com", "*.example.com", false},
		{"wildcard over empty label", "a..example.com", "*.example.com", false},
		{"Unicode folding is not DNS folding", "k.example.com", "K.example.com", false},
		{"literal wildcard inventory subject", "*.example.com", "*.example.com", true},
		{"private underscore name", "node_internal.example.com", "*.example.com", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeCertMismatches(tc.subject, nil, "42", "Private CA", []string{tc.san})
			if (len(got) == 0) != tc.matches {
				t.Fatalf("subject=%q SAN=%q mismatches=%v, want matches=%t", tc.subject, tc.san, got, tc.matches)
			}
		})
	}
}
