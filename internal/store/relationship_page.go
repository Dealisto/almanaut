package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Dealisto/almanaut/internal/domain"
)

// RelationshipPage bounds materialization in SQL. Count and selection share
// the caller's read-only transaction, including for an out-of-range page.
func RelationshipPage(ctx context.Context, tx *sql.Tx, limit, offset int) ([]domain.Relationship, int, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, 0, fmt.Errorf("invalid relationship page")
	}
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM relationships").Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count relationships: %w", err)
	}
	if offset >= total {
		return []domain.Relationship{}, total, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, from_type, from_id, to_type, to_id, kind
		FROM relationships ORDER BY id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query relationship page: %w", err)
	}
	defer rows.Close()
	rels := []domain.Relationship{}
	for rows.Next() {
		rel, err := scanRelationship(rows)
		if err != nil {
			return nil, 0, err
		}
		rels = append(rels, rel)
	}
	return rels, total, rows.Err()
}
