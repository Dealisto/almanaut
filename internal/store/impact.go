package store

import (
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// Impact returns every entity that transitively depends on (startType, startID),
// following only dependency-kind relationships (where the dependent is the "from"
// end and the dependency is the "to" end). The start entity is not included.
// The traversal is breadth-first and cycle-safe.
func Impact(repo *RelationshipRepo, startType string, startID int64) ([]domain.EntityRef, error) {
	refKey := func(t string, id int64) string { return fmt.Sprintf("%s:%d", t, id) }

	// Load every relationship once and index dependency edges by their "to"
	// endpoint, then traverse in memory. The previous BFS ran one ListByTo query
	// per visited node, so a dense dependency subgraph cost one round-trip per
	// node; this is a single query regardless of subgraph size.
	all, err := repo.List()
	if err != nil {
		return nil, err
	}
	dependentsOf := make(map[string][]domain.EntityRef)
	for _, e := range all {
		if !domain.IsDependencyKind(e.Kind) {
			continue
		}
		k := refKey(e.ToType, e.ToID)
		dependentsOf[k] = append(dependentsOf[k], domain.EntityRef{Type: e.FromType, ID: e.FromID})
	}

	visited := map[string]bool{refKey(startType, startID): true}
	result := []domain.EntityRef{}
	queue := []domain.EntityRef{{Type: startType, ID: startID}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, dep := range dependentsOf[refKey(cur.Type, cur.ID)] {
			k := refKey(dep.Type, dep.ID)
			if visited[k] {
				continue
			}
			visited[k] = true
			result = append(result, dep)
			queue = append(queue, dep)
		}
	}
	return result, nil
}
