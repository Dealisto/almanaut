package domain

import "testing"

func TestHardwareValidate(t *testing.T) {
	tests := []struct {
		name    string
		hw      Hardware
		wantErr bool
	}{
		{"ok minimal", Hardware{Name: "APC UPS"}, false},
		{"ok with dates", Hardware{Name: "disk1", PurchaseDate: "2025-01-02", WarrantyEnd: "2028-01-02"}, false},
		{"missing name", Hardware{Kind: "ups"}, true},
		{"blank name", Hardware{Name: "   "}, true},
		{"bad purchase date", Hardware{Name: "x", PurchaseDate: "01-02-2025"}, true},
		{"bad warranty date", Hardware{Name: "x", WarrantyEnd: "soon"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.hw.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
