package domain

import (
	"fmt"
	"strings"
	"time"
)

// Backup describes how something in the homelab is backed up.
type Backup struct {
	ID          int64  `yaml:"id"`
	Source      string `yaml:"source"` // what is backed up
	Destination string `yaml:"destination"`
	Frequency   string `yaml:"frequency"`
	LastRun     string `yaml:"last_run"` // optional YYYY-MM-DD
	Notes       string `yaml:"notes"`
}

// Validate checks the source and (if present) the last-run date.
func (b Backup) Validate() error {
	if strings.TrimSpace(b.Source) == "" {
		return fmt.Errorf("source is required")
	}
	if lr := strings.TrimSpace(b.LastRun); lr != "" {
		if _, err := time.Parse(DateLayout, lr); err != nil {
			return fmt.Errorf("last run must be YYYY-MM-DD, got %q", b.LastRun)
		}
	}
	return nil
}
