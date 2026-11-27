package web

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// BenchmarkCatalogOptions isolates dropdown construction from HTML rendering.
// Every option is retained; unrelated entity fields must not be materialized.
func BenchmarkCatalogOptions(b *testing.B) {
	db, err := store.Open(filepath.Join(b.TempDir(), "options.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db, ""); err != nil {
		b.Fatal(err)
	}
	if err := store.WithTx(db, func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(`INSERT INTO hosts(name,type,notes) VALUES(?,'physical',printf('%01024d',0))`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for i := range 10000 {
			if _, err := stmt.Exec(fmt.Sprintf("host-%05d", i)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	cat := entityCatalog{db: db, resources: []mountable{resource[domain.Host]{
		name: "hosts", sing: "host", repo: store.NewHostRepo(db),
		query: inventoryQuery("hosts", "e.name", "e.name"),
		id:    func(h domain.Host) int64 { return h.ID }, label: func(h domain.Host) string { return h.Name },
	}}}
	b.ReportAllocs()
	for b.Loop() {
		opts, err := cat.options()
		if err != nil {
			b.Fatal(err)
		}
		if len(opts) != 10000 {
			b.Fatalf("options=%d", len(opts))
		}
	}
}
