package domain

import "testing"

func TestSubscriptionValidate(t *testing.T) {
	tests := []struct {
		name    string
		sub     Subscription
		wantErr bool
	}{
		{"ok minimal", Subscription{Name: "Hetzner VPS"}, false},
		{"ok full", Subscription{Name: "JetBrains", Amount: "12.99", Currency: "USD", BillingCycle: "yearly", RenewalDate: "2027-01-02"}, false},
		{"ok zero amount", Subscription{Name: "Free tier", Amount: "0"}, false},
		{"missing name", Subscription{Kind: "vps"}, true},
		{"blank name", Subscription{Name: "   "}, true},
		{"negative amount", Subscription{Name: "x", Amount: "-1"}, true},
		{"non-numeric amount", Subscription{Name: "x", Amount: "abc"}, true},
		{"bad renewal date", Subscription{Name: "x", RenewalDate: "01-02-2027"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.sub.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
