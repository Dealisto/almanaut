package web

import (
	"context"
	"database/sql"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// tagEntities reads tag references and their labels from one read-only snapshot.
// Unrelated inventory and display decorations are never materialized.
func tagEntities(ctx context.Context, db *sql.DB, tags *store.TagRepo, cat entityCatalog, name string) ([]tagEntity, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tagged, err := tags.WithTx(tx).ListByName(name)
	if err != nil {
		return nil, err
	}
	refs := make([]domain.EntityRef, 0, len(tagged))
	for _, tag := range tagged {
		refs = append(refs, domain.EntityRef{Type: tag.EntityType, ID: tag.EntityID})
	}
	labels, err := cat.labelsFor(ctx, tx, refs)
	if err != nil {
		return nil, err
	}
	entities := make([]tagEntity, 0, len(tagged))
	for _, tag := range tagged {
		entities = append(entities, tagEntity{
			Label: labelOrFallback(labels, tag.EntityType, tag.EntityID),
			URL:   cat.path(tag.EntityType, tag.EntityID),
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return entities, nil
}
