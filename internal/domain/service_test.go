package domain

import "testing"

func TestServiceValidate(t *testing.T) {
	tests := []struct {
		name    string
		svc     Service
		wantErr bool
	}{
		{"valid", Service{Name: "jellyfin", Kind: "container"}, false},
		{"valid native", Service{Name: "nginx", Kind: "native"}, false},
		{"empty name", Service{Name: "  ", Kind: "container"}, true},
		{"bad kind", Service{Name: "x", Kind: "wasm"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.svc.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
