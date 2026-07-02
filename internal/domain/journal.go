package domain

import (
	"fmt"
	"strings"
)

// Journal entry kinds.
const (
	JournalInfo     = "info"
	JournalSuccess  = "success"
	JournalWarning  = "warning"
	JournalIncident = "incident"
)

// JournalKinds lists the valid kinds in display order (feeds the form select).
var JournalKinds = []string{JournalInfo, JournalSuccess, JournalWarning, JournalIncident}

// JournalEntry is one manual, timestamped note attached to an entity.
type JournalEntry struct {
	ID         int64  `yaml:"id" json:"id"`
	EntityType string `yaml:"entity_type" json:"entity_type"`
	EntityID   int64  `yaml:"entity_id" json:"entity_id"`
	Kind       string `yaml:"kind" json:"kind"`
	Body       string `yaml:"body" json:"body"`
	CreatedAt  string `yaml:"created_at" json:"created_at"`
}

// Validate checks the entry has a known kind and a non-empty body.
func (e JournalEntry) Validate() error {
	if !contains(JournalKinds, e.Kind) {
		return fmt.Errorf("invalid journal kind %q", e.Kind)
	}
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Errorf("journal body is required")
	}
	return nil
}
