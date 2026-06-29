package domain

import "testing"

func TestRelationshipValidate(t *testing.T) {
	base := Relationship{FromType: "service", FromID: 1, ToType: "host", ToID: 2, Kind: "runs on"}
	tests := []struct {
		name    string
		mutate  func(r *Relationship)
		wantErr bool
	}{
		{"valid", func(r *Relationship) {}, false},
		{"bad from type", func(r *Relationship) { r.FromType = "widget" }, true},
		{"bad to type", func(r *Relationship) { r.ToType = "widget" }, true},
		{"bad kind", func(r *Relationship) { r.Kind = "loves" }, true},
		{"zero from id", func(r *Relationship) { r.FromID = 0 }, true},
		{"self reference", func(r *Relationship) { r.ToType = "service"; r.ToID = 1 }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mutate(&r)
			if err := r.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestIsDependencyKind(t *testing.T) {
	if !IsDependencyKind("runs on") {
		t.Error(`"runs on" should be a dependency kind`)
	}
	if IsDependencyKind("backed up by") {
		t.Error(`"backed up by" should NOT be a dependency kind`)
	}
}

func TestRelationshipHardwareEndpoint(t *testing.T) {
	r := Relationship{FromType: "hardware", FromID: 1, ToType: "host", ToID: 2, Kind: "runs on"}
	if err := r.Validate(); err != nil {
		t.Fatalf("hardware endpoint should be valid, got %v", err)
	}
}

func TestRelationshipSubscriptionEndpoint(t *testing.T) {
	r := Relationship{FromType: "subscription", FromID: 1, ToType: "host", ToID: 2, Kind: "runs on"}
	if err := r.Validate(); err != nil {
		t.Fatalf("subscription endpoint should be valid, got %v", err)
	}
}
