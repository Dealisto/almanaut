package domain

import "testing"

func TestDomainValidate(t *testing.T) {
	tests := []struct {
		name    string
		d       Domain
		wantErr bool
	}{
		{"valid", Domain{FQDN: "jellyfin.example.com"}, false},
		{"valid apex", Domain{FQDN: "example.com"}, false},
		{"empty fqdn", Domain{FQDN: "  "}, true},
		{"no dot", Domain{FQDN: "localhost"}, true},
		{"space in fqdn", Domain{FQDN: "bad domain.com"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.d.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
