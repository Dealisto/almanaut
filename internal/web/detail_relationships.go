package web

import (
	"context"
	"database/sql"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// detailRelationships reads the edges and their opposite-endpoint labels in
// one read-only snapshot. Other detail sections have independent reads. Only
// referenced labels are loaded; the full catalog is not needed for rendering.
func detailRelationships(ctx context.Context, db *sql.DB, cat entityCatalog, rels *store.RelationshipRepo, entityType string, entityID int64) ([]domain.Relationship, map[string]string, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	edges, err := rels.WithTx(tx).ListForEntity(entityType, entityID)
	if err != nil {
		return nil, nil, err
	}
	refs := make([]domain.EntityRef, 0, len(edges))
	for _, edge := range edges {
		if edge.FromType == entityType && edge.FromID == entityID {
			refs = append(refs, domain.EntityRef{Type: edge.ToType, ID: edge.ToID})
		} else {
			refs = append(refs, domain.EntityRef{Type: edge.FromType, ID: edge.FromID})
		}
	}
	labels, err := cat.labelsFor(ctx, tx, refs)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return edges, labels, nil
}
