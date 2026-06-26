package domain

import "testing"

func TestBackupValidate(t *testing.T) {
	tests := []struct {
		name    string
		b       Backup
		wantErr bool
	}{
		{"valid", Backup{Source: "nextcloud-data", Destination: "B2", LastRun: "2026-06-20"}, false},
		{"valid no lastrun", Backup{Source: "nextcloud-data"}, false},
		{"empty source", Backup{Source: "  "}, true},
		{"bad lastrun", Backup{Source: "x", LastRun: "yesterday"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.b.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
