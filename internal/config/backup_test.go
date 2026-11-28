package config

import (
	"strings"
	"testing"
	"time"
)

func TestBackupConfiguration(t *testing.T) {
	for _, key := range []string{"ALMANAUT_BACKUP_DIR", "ALMANAUT_BACKUP_INTERVAL", "ALMANAUT_BACKUP_TIMEOUT", "ALMANAUT_BACKUP_KEEP"} {
		t.Setenv(key, "")
	}
	cfg := mustLoad(t)
	if cfg.BackupDir != "" || cfg.BackupInterval != 24*time.Hour || cfg.BackupTimeout != 10*time.Minute || cfg.BackupKeep != 7 {
		t.Fatalf("unexpected backup defaults: %q %v %v %d", cfg.BackupDir, cfg.BackupInterval, cfg.BackupTimeout, cfg.BackupKeep)
	}
	t.Setenv("ALMANAUT_BACKUP_DIR", "/data/backup copies")
	t.Setenv("ALMANAUT_BACKUP_INTERVAL", "6h")
	t.Setenv("ALMANAUT_BACKUP_TIMEOUT", "30m")
	t.Setenv("ALMANAUT_BACKUP_KEEP", "14")
	cfg = mustLoad(t)
	if cfg.BackupDir != "/data/backup copies" || cfg.BackupInterval != 6*time.Hour || cfg.BackupTimeout != 30*time.Minute || cfg.BackupKeep != 14 {
		t.Fatal("explicit backup configuration was not preserved")
	}
}

func TestBackupConfigurationRejectsInvalidExplicitValues(t *testing.T) {
	for _, key := range []string{"ALMANAUT_BACKUP_INTERVAL", "ALMANAUT_BACKUP_TIMEOUT", "ALMANAUT_BACKUP_KEEP"} {
		t.Setenv(key, "")
	}
	for _, tc := range []struct{ key, value string }{
		{"ALMANAUT_BACKUP_INTERVAL", "0s"},
		{"ALMANAUT_BACKUP_INTERVAL", "-24h"},
		{"ALMANAUT_BACKUP_TIMEOUT", "forever"},
		{"ALMANAUT_BACKUP_TIMEOUT", "0"},
		{"ALMANAUT_BACKUP_KEEP", "0"},
		{"ALMANAUT_BACKUP_KEEP", "-1"},
		{"ALMANAUT_BACKUP_KEEP", "7.5"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("invalid %s was not rejected: %v", tc.key, err)
			}
		})
	}
}
