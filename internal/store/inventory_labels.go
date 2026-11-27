package store

import (
	"context"
	"database/sql"
	"fmt"
)

// InventoryLabels reads only labels for the supplied positive IDs. SQL metadata
// comes from the trusted resource catalog, just as for InventoryIDs/Search;
// callers never interpolate submitted entity types, IDs or field names. Missing
// IDs are omitted. Label expressions preserve composite owner/port/NIC labels.
func InventoryLabels(ctx context.Context, tx *sql.Tx, spec InventoryQuery, ids []int64) (map[int64]string, error) {
	labels := make(map[int64]string)
	err := eachIDBatch(ids, func(placeholders string, args []any) error {
		rows, err := tx.QueryContext(ctx, `SELECT e.id, `+textExpression(spec.Label)+` FROM `+spec.Table+` e WHERE e.id IN (`+placeholders+`)`, args...)
		if err != nil {
			return fmt.Errorf("query inventory labels: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var label string
			if err := rows.Scan(&id, &label); err != nil {
				return fmt.Errorf("scan inventory label: %w", err)
			}
			labels[id] = label
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return labels, nil
}
