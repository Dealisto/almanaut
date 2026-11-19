package domain

import "testing"

func TestHostValidate(t *testing.T) {
	tests := []struct {
		name    string
		host    Host
		wantErr bool
	}{
		{"valid minimal", Host{Name: "web01", Type: "physical"}, false},
		{"valid with ips", Host{Name: "web01", Type: "vm", IPs: []string{"10.0.0.5", "fe80::1"}}, false},
		{"empty name", Host{Name: "  ", Type: "physical"}, true},
		{"custom type name", Host{Name: "web01", Type: "toaster"}, false},
		{"bad type name", Host{Name: "web01", Type: "bad type"}, true},
		{"empty type", Host{Name: "web01"}, true},
		{"bad ip", Host{Name: "web01", Type: "vm", IPs: []string{"not-an-ip"}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.host.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
