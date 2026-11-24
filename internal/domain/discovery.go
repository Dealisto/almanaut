package domain

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// DiscoveryBinding identifies an imported resource independently of either
// its source name or its editable inventory name. Source is a stable kind:id
// key, never an endpoint URL or credential.
type DiscoveryBinding struct {
	Source     string `yaml:"source" json:"source"`
	ExternalID string `yaml:"external_id" json:"external_id"`
	EntityType string `yaml:"entity_type" json:"entity_type"`
	EntityID   int64  `yaml:"entity_id" json:"entity_id"`
}

var discoverySourceRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}:[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func (b DiscoveryBinding) Validate() error {
	if !discoverySourceRE.MatchString(b.Source) {
		return fmt.Errorf("discovery source must be a stable kind:id key")
	}
	if strings.TrimSpace(b.ExternalID) == "" || len(b.ExternalID) > 512 || strings.IndexFunc(b.ExternalID, unicode.IsControl) >= 0 {
		return fmt.Errorf("discovery resource ID must be nonempty, at most 512 bytes, and contain no control characters")
	}
	if b.EntityType != "host" && b.EntityType != "service" {
		return fmt.Errorf("discovery binding entity type must be host or service")
	}
	if b.EntityID <= 0 {
		return fmt.Errorf("discovery binding entity ID must be positive")
	}
	return nil
}
