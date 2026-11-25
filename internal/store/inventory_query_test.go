package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkInventoryPage(b *testing.B) {
	for _, size := range []int{1000, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			db, err := Open(filepath.Join(b.TempDir(), "inventory.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			if err := Migrate(db, ""); err != nil {
				b.Fatal(err)
			}
			err = WithTx(db, func(tx *sql.Tx) error {
				stmt, err := tx.Prepare(`INSERT INTO hosts(name,type,notes) VALUES (?,'physical',?)`)
				if err != nil {
					return err
				}
				defer stmt.Close()
				for i := 0; i < size; i++ {
					if _, err := stmt.Exec(fmt.Sprintf("host-%06d", i), strings.Repeat("inventory context ", 20)); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			spec := InventoryQuery{Type: "host", Table: "hosts", Label: "e.name", Order: "e.name", Fields: []InventoryField{{"Name", "e.name"}}, Search: []string{"e.name", "e.notes"}}
			for _, filtered := range []bool{false, true} {
				b.Run(fmt.Sprintf("filtered=%t", filtered), func(b *testing.B) {
					filter := InventoryFilter{}
					if filtered {
						filter.Query = "inventory"
					}
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
						if err != nil {
							b.Fatal(err)
						}
						ids, total, err := InventoryIDs(context.Background(), tx, spec, filter, 50, 0)
						if err != nil {
							tx.Rollback()
							b.Fatal(err)
						}
						if len(ids) != 50 || total != size {
							tx.Rollback()
							b.Fatalf("page=%d total=%d", len(ids), total)
						}
						// Hydrate only selected records, just like the HTTP handlers.
						for _, id := range ids {
							if _, err := NewHostRepo(db).GetTx(tx, id); err != nil {
								tx.Rollback()
								b.Fatal(err)
							}
						}
						if err := tx.Commit(); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
