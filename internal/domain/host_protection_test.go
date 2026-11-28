package domain

import "testing"

func TestHostAgentProtectionValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fields  []string
		invalid bool
	}{
		{"omitted", nil, false},
		{"empty", []string{}, false},
		{"all", []string{"ips", "os", "cpu", "ram", "disk"}, false},
		{"unknown", []string{"notes"}, true},
		{"blank", []string{""}, true},
		{"case sensitive", []string{"IPs"}, true},
		{"duplicate", []string{"ips", "ips"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := Host{Name: "server", Type: "physical", AgentProtectedFields: tc.fields}
			if err := h.Validate(); (err != nil) != tc.invalid {
				t.Fatalf("Validate() = %v, invalid = %v", err, tc.invalid)
			}
		})
	}
}
