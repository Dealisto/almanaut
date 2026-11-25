package store

import (
	"fmt"
	"strings"
)

// eachIDBatch deduplicates positive IDs and bounds query parameters even when
// a caller decorates an entire export rather than one inventory page.
func eachIDBatch(ids []int64, query func(string, []any) error) error {
	seen := make(map[int64]bool, len(ids))
	args := make([]any, 0, 500)
	flush := func() error {
		if len(args) == 0 {
			return nil
		}
		return query(strings.TrimSuffix(strings.Repeat("?,", len(args)), ","), args)
	}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		args = append(args, id)
		if len(args) == cap(args) {
			if err := flush(); err != nil {
				return err
			}
			args = args[:0]
		}
	}
	return flush()
}

// entityNames resolves only referenced rows. Table names are internal constants;
// IDs are parameters. A single port/NIC read must not load the whole inventory.
func entityNames(db DBTX, table string, ids []int64) (map[int64]string, error) {
	names := make(map[int64]string)
	err := eachIDBatch(ids, func(placeholders string, args []any) error {
		rows, err := db.Query(`SELECT id, name FROM `+table+` WHERE id IN (`+placeholders+`)`, args...)
		if err != nil {
			return fmt.Errorf("query %s names: %w", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				return fmt.Errorf("scan %s name: %w", table, err)
			}
			names[id] = name
		}
		return rows.Err()
	})
	return names, err
}
