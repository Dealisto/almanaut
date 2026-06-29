package domain

import "fmt"

// EntityTypes is the closed set of entity types that can participate in a relationship.
var EntityTypes = []string{"host", "service", "network", "domain", "certificate", "backup", "hardware", "subscription", "account"}

// RelationshipKinds is the closed set of relationship labels ("from <kind> to").
var RelationshipKinds = []string{"runs on", "connected to", "exposed via", "secured by", "backed up by", "depends on"}

// dependencyKinds are the kinds that mean "from needs to to function", used by
// impact analysis. "backed up by" is intentionally excluded.
var dependencyKinds = map[string]bool{
	"runs on":      true,
	"connected to": true,
	"exposed via":  true,
	"secured by":   true,
	"depends on":   true,
}

// EntityRef is a lightweight (type, id) reference to any entity.
type EntityRef struct {
	Type string
	ID   int64
}

// Relationship is a directional, typed edge between two entities: "from kind to".
type Relationship struct {
	ID       int64  `yaml:"id"`
	FromType string `yaml:"from_type"`
	FromID   int64  `yaml:"from_id"`
	ToType   string `yaml:"to_type"`
	ToID     int64  `yaml:"to_id"`
	Kind     string `yaml:"kind"`
}

// Validate checks the endpoint types, the kind, and that the edge is well-formed.
func (r Relationship) Validate() error {
	if !contains(EntityTypes, r.FromType) {
		return fmt.Errorf("invalid from type %q", r.FromType)
	}
	if !contains(EntityTypes, r.ToType) {
		return fmt.Errorf("invalid to type %q", r.ToType)
	}
	if !contains(RelationshipKinds, r.Kind) {
		return fmt.Errorf("invalid kind %q", r.Kind)
	}
	if r.FromID <= 0 || r.ToID <= 0 {
		return fmt.Errorf("both ends must reference an entity")
	}
	if r.FromType == r.ToType && r.FromID == r.ToID {
		return fmt.Errorf("an entity cannot relate to itself")
	}
	return nil
}

// IsDependencyKind reports whether kind represents a functional dependency
// (used by impact analysis).
func IsDependencyKind(kind string) bool {
	return dependencyKinds[kind]
}
