package domain

import (
	"fmt"
	"strings"
)

// SavedView is a named filter/sort query string for one entity type's list
// page, owned by a user. The Query is a raw URL query string (e.g.
// "sort=Name&dir=asc&tag=prod") replayed against the list route.
type SavedView struct {
	ID         int64
	UserID     int64
	EntityType string
	Name       string
	Query      string
	CreatedAt  string
}

// Validate checks the name is present and the entity type is known.
func (v SavedView) Validate() error {
	if strings.TrimSpace(v.Name) == "" {
		return fmt.Errorf("view name is required")
	}
	if !contains(EntityTypes, v.EntityType) {
		return fmt.Errorf("invalid entity type %q", v.EntityType)
	}
	return nil
}
