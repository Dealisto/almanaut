package domain

import "testing"

func TestAccountValidate(t *testing.T) {
	tests := []struct {
		name    string
		acc     Account
		wantErr bool
	}{
		{"ok minimal", Account{Name: "Proxmox root"}, false},
		{"ok full", Account{
			Name: "Grafana admin", Kind: "admin", Username: "admin",
			PasswordManager: "Bitwarden", SecretRef: "Homelab > Grafana",
			URL: "https://grafana.lan", Status: "active", Notes: "shared",
		}, false},
		{"missing name", Account{Kind: "admin"}, true},
		{"blank name", Account{Name: "   "}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.acc.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
