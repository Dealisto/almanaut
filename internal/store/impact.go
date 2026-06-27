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

	visited := map[string]bool{refKey(startType, startID): true}
	result := []domain.EntityRef{}
	queue := []domain.EntityRef{{Type: startType, ID: startID}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		edges, err := repo.ListByTo(cur.Type, cur.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if !domain.IsDependencyKind(e.Kind) {
				continue
			}
			k := refKey(e.FromType, e.FromID)
			if visited[k] {
				continue
			}
			visited[k] = true
			ref := domain.EntityRef{Type: e.FromType, ID: e.FromID}
			result = append(result, ref)
			queue = append(queue, ref)
		}
	}
	return result, nil
}
