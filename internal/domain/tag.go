package domain

import (
	"fmt"
	"strings"
)

// Tag is a normalized label attached to an entity.
type Tag struct {
	ID         int64
	EntityType string
	EntityID   int64
	Name       string
}

// NormalizeTag trims whitespace, strips a leading '#', and lowercases.
func NormalizeTag(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	return strings.ToLower(strings.TrimSpace(s))
}

// Validate checks the entity reference and that the (normalized) name is non-empty.
func (t Tag) Validate() error {
	if !contains(EntityTypes, t.EntityType) {
		return fmt.Errorf("invalid entity type %q", t.EntityType)
	}
	if t.EntityID <= 0 {
		return fmt.Errorf("entity id is required")
	}
	if NormalizeTag(t.Name) == "" {
		return fmt.Errorf("tag name is required")
	}
	return nil
}
