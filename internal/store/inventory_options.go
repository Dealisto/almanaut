package store

import (
	"context"
	"database/sql"
	"fmt"
)

// InventoryOption contains only the fields needed for an entity selector.
type InventoryOption struct {
	ID    int64
	Label string
}

// InventoryOptions preserves the repository's ordering using trusted catalog
// metadata. It avoids materializing entity fields or unrelated decorations.
func InventoryOptions(ctx context.Context, tx *sql.Tx, spec InventoryQuery) ([]InventoryOption, error) {
	rows, err := tx.QueryContext(ctx, `SELECT e.id, `+textExpression(spec.Label)+` FROM `+spec.Table+` e ORDER BY `+spec.Order)
	if err != nil {
		return nil, fmt.Errorf("query inventory options: %w", err)
	}
	defer rows.Close()
	var options []InventoryOption
	for rows.Next() {
		var option InventoryOption
		if err := rows.Scan(&option.ID, &option.Label); err != nil {
			return nil, fmt.Errorf("scan inventory option: %w", err)
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return options, nil
}
