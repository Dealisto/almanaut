package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CustomFieldKind is the data type of a custom field's value.
type CustomFieldKind string

const (
	KindText   CustomFieldKind = "text"
	KindNumber CustomFieldKind = "number"
	KindBool   CustomFieldKind = "bool"
	KindDate   CustomFieldKind = "date"
)

// CustomFieldKinds is the closed set of supported kinds (form-select order).
var CustomFieldKinds = []CustomFieldKind{KindText, KindNumber, KindBool, KindDate}

// CustomFieldDef defines a user-created field attached to one entity type.
type CustomFieldDef struct {
	ID         int64           `yaml:"id"`
	EntityType string          `yaml:"entity_type"`
	Name       string          `yaml:"name"`
	Label      string          `yaml:"label"`
	Kind       CustomFieldKind `yaml:"kind"`
	CreatedAt  string          `yaml:"created_at"`
}

// CustomFieldValue is one entity's value for a definition, denormalised with the
// definition's name/label/kind for convenient rendering.
type CustomFieldValue struct {
	DefID int64           `yaml:"def_id"`
	Name  string          `yaml:"name"`
	Label string          `yaml:"label"`
	Kind  CustomFieldKind `yaml:"kind"`
	Value string          `yaml:"value"`
}

var customFieldNameRE = regexp.MustCompile(`^[a-z0-9_]+$`)

// SlugifyCustomField derives a machine name from a label: lowercased, runs of
// non-alphanumeric characters collapsed to a single underscore, trimmed.
func SlugifyCustomField(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		case b.Len() > 0 && !lastUnderscore:
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func validCustomFieldKind(k CustomFieldKind) bool {
	for _, x := range CustomFieldKinds {
		if x == k {
			return true
		}
	}
	return false
}

// Validate checks the definition's entity type, slug name, label, and kind.
func (d CustomFieldDef) Validate() error {
	if !contains(EntityTypes, d.EntityType) {
		return fmt.Errorf("invalid entity type %q", d.EntityType)
	}
	if !customFieldNameRE.MatchString(d.Name) {
		return fmt.Errorf("name must match [a-z0-9_]+")
	}
	if strings.TrimSpace(d.Label) == "" {
		return fmt.Errorf("label is required")
	}
	if !validCustomFieldKind(d.Kind) {
		return fmt.Errorf("invalid kind %q", d.Kind)
	}
	return nil
}

// ValidateCustomFieldValue validates and canonicalises a raw submitted value for
// the given kind. Empty text/number/date is allowed and returned as "" (the
// caller then deletes any stored value). A bool is always canonicalised to
// "true"/"false".
func ValidateCustomFieldValue(kind CustomFieldKind, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	switch kind {
	case KindText:
		return raw, nil
	case KindNumber:
		if raw == "" {
			return "", nil
		}
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return "", fmt.Errorf("must be a number")
		}
		return raw, nil
	case KindBool:
		switch strings.ToLower(raw) {
		case "on", "true", "1", "yes", "y":
			return "true", nil
		default:
			return "false", nil
		}
	case KindDate:
		if err := validateOptionalDate("value", raw); err != nil {
			return "", err
		}
		return raw, nil
	default:
		return "", fmt.Errorf("invalid kind %q", kind)
	}
}
