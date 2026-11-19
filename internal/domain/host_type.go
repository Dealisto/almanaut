package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// HostType is a catalog entry. Name is stable; Label may be renamed.
// These are settings, not entities with relationships or custom fields.
type HostType struct {
	Name  string `yaml:"name" json:"name"`
	Label string `yaml:"label" json:"label"`
}

var hostTypeNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (t HostType) Validate() error {
	if !hostTypeNameRE.MatchString(t.Name) {
		return fmt.Errorf("type name must contain 1–64 lowercase letters, digits, underscores or hyphens, starting with a letter or digit")
	}
	if strings.TrimSpace(t.Label) == "" {
		return fmt.Errorf("label is required")
	}
	if len(t.Label) > 100 {
		return fmt.Errorf("label must be at most 100 bytes")
	}
	return nil
}

// Builtin types have stable names required by discovery and agent reports.
func (t HostType) Builtin() bool { return contains(HostTypes, t.Name) }

func DefaultHostTypes() []HostType {
	types := make([]HostType, 0, len(HostTypes))
	for _, name := range HostTypes {
		types = append(types, HostType{Name: name, Label: name})
	}
	return types
}
