package domain

import "testing"

func TestCertificateValidate(t *testing.T) {
	tests := []struct {
		name    string
		c       Certificate
		wantErr bool
	}{
		{"valid", Certificate{Subject: "*.example.com", ExpiresOn: "2027-01-15"}, false},
		{"empty subject", Certificate{Subject: " ", ExpiresOn: "2027-01-15"}, true},
		{"empty date", Certificate{Subject: "x", ExpiresOn: ""}, true},
		{"bad date", Certificate{Subject: "x", ExpiresOn: "15-01-2027"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.c.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
