package domain

import (
	"fmt"
	"strings"
)

// Backup describes how something in the homelab is backed up.
type Backup struct {
	ID          int64  `yaml:"id" json:"id"`
	Source      string `yaml:"source" json:"source"` // what is backed up
	Destination string `yaml:"destination" json:"destination"`
	Frequency   string `yaml:"frequency" json:"frequency"`
	LastRun     string `yaml:"last_run" json:"last_run"` // optional YYYY-MM-DD
	Notes       string `yaml:"notes" json:"notes"`
}

// Validate checks the source and (if present) the last-run date.
func (b Backup) Validate() error {
	if strings.TrimSpace(b.Source) == "" {
		return fmt.Errorf("source is required")
	}
	return validateOptionalDate("last run", b.LastRun)
}
