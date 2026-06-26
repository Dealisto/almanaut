package domain

import "testing"

func TestNetworkValidate(t *testing.T) {
	tests := []struct {
		name    string
		net     Network
		wantErr bool
	}{
		{"valid", Network{Name: "lan", CIDR: "10.0.0.0/24"}, false},
		{"valid with gateway", Network{Name: "lan", CIDR: "10.0.0.0/24", Gateway: "10.0.0.1"}, false},
		{"empty name", Network{Name: " ", CIDR: "10.0.0.0/24"}, true},
		{"empty cidr", Network{Name: "lan", CIDR: ""}, true},
		{"bad cidr", Network{Name: "lan", CIDR: "10.0.0.0/99"}, true},
		{"bad gateway", Network{Name: "lan", CIDR: "10.0.0.0/24", Gateway: "nope"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.net.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
