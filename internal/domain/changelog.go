package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Change actions recorded in the changelog.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
	ActionImport = "import"
)

// FieldChange is one field's before/after value. For a create, Old is empty and
// New is the initial value.
type FieldChange struct {
	Field string `json:"field"`
	Old   string `json:"old"`
	New   string `json:"new"`
}

// Diff reports the fields that differ between old and new. Both are marshalled
// to their JSON representation (the snake_case tags the API already uses) and
// compared key by key, so a single code path covers every entity type. The
// synthetic "id" field is never reported. Results are sorted by field name for
// a stable log.
func Diff(old, new any) ([]FieldChange, error) {
	om, err := toMap(old)
	if err != nil {
		return nil, err
	}
	nm, err := toMap(new)
	if err != nil {
		return nil, err
	}
	keys := map[string]struct{}{}
	for k := range om {
		keys[k] = struct{}{}
	}
	for k := range nm {
		keys[k] = struct{}{}
	}
	var changes []FieldChange
	for k := range keys {
		if k == "id" {
			continue
		}
		ov, nv := renderValue(om[k]), renderValue(nm[k])
		if ov != nv {
			changes = append(changes, FieldChange{Field: k, Old: ov, New: nv})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Field < changes[j].Field })
	return changes, nil
}

func toMap(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal for diff: %w", err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshal for diff: %w", err)
	}
	return m, nil
}

// renderValue turns a JSON-decoded value into the string stored in the log.
// Slices (e.g. a host's IPs) render comma-separated; everything else uses its
// natural formatting.
func renderValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = fmt.Sprintf("%v", e)
		}
		return strings.Join(parts, ", ")
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}
