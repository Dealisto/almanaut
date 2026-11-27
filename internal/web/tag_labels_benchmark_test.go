package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func BenchmarkTagDrilldownSingleReference(b *testing.B) {
	db, err := store.Open(filepath.Join(b.TempDir(), "tags.db"))
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
	tags := store.NewTagRepo(db)
	if err := tags.Add(domain.Tag{EntityType: "host", EntityID: 1, Name: "selected"}); err != nil {
		b.Fatal(err)
	}
	cat := entityCatalog{resources: []mountable{resource[domain.Host]{
		name: "hosts", sing: "host", repo: store.NewHostRepo(db),
		query: inventoryQuery("hosts", "e.name", "e.name"),
		id:    func(h domain.Host) int64 { return h.ID }, label: func(h domain.Host) string { return h.Name },
	}}}
	handler := tagsOverview(db, tags, cat)
	b.ReportAllocs()
	for b.Loop() {
		response := httptest.NewRecorder()
		handler(response, httptest.NewRequest(http.MethodGet, "/tags?name=selected", nil))
		if response.Code != http.StatusOK {
			b.Fatalf("drilldown status=%d", response.Code)
		}
	}
}
