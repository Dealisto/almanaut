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
	ID         int64  `json:"id" yaml:"id"`
	EntityType string `json:"entity_type" yaml:"entity_type"`
	EntityID   int64  `json:"entity_id" yaml:"entity_id"`
	Kind       string `json:"kind" yaml:"kind"`
	Body       string `json:"body" yaml:"body"`
	CreatedAt  string `json:"created_at" yaml:"created_at"`
}

// Validate checks the entry has a known kind and a non-empty body.
func (e JournalEntry) Validate() error {
	switch e.Kind {
	case JournalInfo, JournalSuccess, JournalWarning, JournalIncident:
	default:
		return fmt.Errorf("invalid journal kind %q", e.Kind)
	}
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Errorf("journal body is required")
	}
	return nil
}
