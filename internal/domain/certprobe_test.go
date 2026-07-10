package domain

import (
	"strings"
	"testing"
)

func TestComputeCertMismatches(t *testing.T) {
	prev := &CertProbeStatus{Serial: "aa", Issuer: "Old CA"}
	cases := []struct {
		name                   string
		subject                string
		prev                   *CertProbeStatus
		serial, issuer         string
		sans                   []string
		wantContainsSubstrings []string
		wantEmpty              bool
	}{
		{"first probe, subject in SANs", "example.com", nil, "bb", "CA", []string{"example.com"}, nil, true},
		{"first probe, wildcard covers subject", "app.example.com", nil, "bb", "CA", []string{"*.example.com"}, nil, true},
		{"subject not in SANs", "example.com", nil, "bb", "CA", []string{"other.com"}, []string{"not in served SANs"}, false},
		{"empty subject skips SAN rule", "", nil, "bb", "CA", []string{"other.com"}, nil, true},
		{"serial changed", "example.com", prev, "bb", "Old CA", []string{"example.com"}, []string{"serial changed"}, false},
		{"issuer changed", "example.com", prev, "aa", "New CA", []string{"example.com"}, []string{"issuer changed"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ComputeCertMismatches(c.subject, c.prev, c.serial, c.issuer, c.sans)
			if c.wantEmpty {
				if len(got) != 0 {
					t.Fatalf("want no mismatches, got %v", got)
				}
				return
			}
			for _, sub := range c.wantContainsSubstrings {
				found := false
				for _, g := range got {
					if strings.Contains(g, sub) {
						found = true
					}
				}
				if !found {
					t.Fatalf("want a mismatch containing %q, got %v", sub, got)
				}
			}
		})
	}
}

func TestSanCoversWildcard(t *testing.T) {
	cases := []struct {
		name string
		sans []string
		host string
		want bool
	}{
		{"wildcard matches direct subdomain", []string{"*.example.com"}, "a.example.com", true},
		{"wildcard does not match bare domain", []string{"*.example.com"}, "example.com", false},
		{"wildcard does not match nested subdomain", []string{"*.example.com"}, "a.b.example.com", false},
		{"exact match is case-insensitive", []string{"Example.com"}, "example.com", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanCovers(c.sans, c.host); got != c.want {
				t.Fatalf("sanCovers(%v, %q) = %v, want %v", c.sans, c.host, got, c.want)
			}
		})
	}
}
