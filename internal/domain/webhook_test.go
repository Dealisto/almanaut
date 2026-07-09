package domain

import "testing"

func TestWebhookMatches(t *testing.T) {
	tests := []struct {
		name       string
		hook       Webhook
		entityType string
		action     string
		want       bool
	}{
		{"empty filters match all", Webhook{}, "host", "created", true},
		{"type in set", Webhook{EntityTypes: []string{"host", "service"}}, "host", "updated", true},
		{"type not in set", Webhook{EntityTypes: []string{"service"}}, "host", "updated", false},
		{"action in set", Webhook{Events: []string{"deleted"}}, "host", "deleted", true},
		{"action not in set", Webhook{Events: []string{"created"}}, "host", "deleted", false},
		{"both must match", Webhook{EntityTypes: []string{"host"}, Events: []string{"created"}}, "host", "deleted", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.hook.Matches(tt.entityType, tt.action); got != tt.want {
				t.Errorf("Matches(%q, %q) = %v, want %v", tt.entityType, tt.action, got, tt.want)
			}
		})
	}
}
