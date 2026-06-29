package web

import (
	"fmt"
	"strconv"
	"strings"
)

// entityOption is one selectable entity in a relationship dropdown.
type entityOption struct {
	Value string // "type:id"
	Label string // "host: proxmox"
	Type  string
	ID    int64
}

// entityCatalog resolves (type,id) references to human labels by aggregating
// every registered entity resource.
type entityCatalog struct {
	resources []mountable
}

func entityOptionOf(typ string, id int64, name string) entityOption {
	return entityOption{
		Value: fmt.Sprintf("%s:%d", typ, id),
		Label: fmt.Sprintf("%s: %s", typ, name),
		Type:  typ,
		ID:    id,
	}
}

// options returns every entity across all registered types as selectable options.
func (c entityCatalog) options() ([]entityOption, error) {
	var opts []entityOption
	for _, rs := range c.resources {
		got, err := rs.options()
		if err != nil {
			return nil, err
		}
		opts = append(opts, got...)
	}
	return opts, nil
}

// labelMap builds a lookup from each option's "type:id" value to its label,
// used to resolve relationship endpoints and tagged entities to human names.
func labelMap(opts []entityOption) map[string]string {
	labels := make(map[string]string, len(opts))
	for _, o := range opts {
		labels[o.Value] = o.Label
	}
	return labels
}

// parseRef splits a "type:id" reference string.
func parseRef(s string) (string, int64, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", 0, fmt.Errorf("invalid entity reference %q", s)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid entity reference %q", s)
	}
	return parts[0], id, nil
}

// labelOrFallback resolves (typ,id) to a label, or a "(deleted)" placeholder
// when the entity no longer exists.
func labelOrFallback(labels map[string]string, typ string, id int64) string {
	key := fmt.Sprintf("%s:%d", typ, id)
	if l, ok := labels[key]; ok {
		return l
	}
	return key + " (deleted)"
}
