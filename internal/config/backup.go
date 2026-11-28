package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Backup settings fail closed on invalid explicit values: silently falling
// back could retain fewer snapshots or run less often than the operator expects.
func loadBackupConfig(cfg *Config) error {
	cfg.BackupDir = os.Getenv("ALMANAUT_BACKUP_DIR")
	cfg.BackupInterval = 24 * time.Hour
	cfg.BackupTimeout = 10 * time.Minute
	cfg.BackupKeep = 7
	for _, setting := range []struct {
		name string
		out  *time.Duration
	}{
		{"ALMANAUT_BACKUP_INTERVAL", &cfg.BackupInterval},
		{"ALMANAUT_BACKUP_TIMEOUT", &cfg.BackupTimeout},
	} {
		if raw := strings.TrimSpace(os.Getenv(setting.name)); raw != "" {
			value, err := time.ParseDuration(raw)
			if err != nil || value <= 0 {
				return fmt.Errorf("%s must be a positive Go duration", setting.name)
			}
			*setting.out = value
		}
	}
	if raw := strings.TrimSpace(os.Getenv("ALMANAUT_BACKUP_KEEP")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			return fmt.Errorf("ALMANAUT_BACKUP_KEEP must be a positive integer")
		}
		cfg.BackupKeep = value
	}
	return nil
}
