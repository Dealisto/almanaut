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

// BenchmarkDetailSingleRelationship measures the shared detail renderer and its
// normal tags, relationship, journal and history reads, graph and HTML rendering.
// Optional entity-specific extras are empty to isolate relationship label cost.
func BenchmarkDetailSingleRelationship(b *testing.B) {
	db, err := store.Open(filepath.Join(b.TempDir(), "detail.db"))
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
	hardware := store.NewHardwareRepo(db)
	if _, err := hardware.Create(domain.Hardware{Name: "switch", Kind: "switch"}); err != nil {
		b.Fatal(err)
	}
	rels := store.NewRelationshipRepo(db)
	if _, err := rels.Create(domain.Relationship{FromType: "service", FromID: 1, ToType: "hardware", ToID: 1, Kind: "depends on"}); err != nil {
		b.Fatal(err)
	}
	cat := entityCatalog{resources: []mountable{
		resource[domain.Host]{
			name: "hosts", sing: "host", repo: store.NewHostRepo(db),
			query: inventoryQuery("hosts", "e.name", "e.name"),
			id:    func(h domain.Host) int64 { return h.ID }, label: func(h domain.Host) string { return h.Name },
		},
		resource[domain.Hardware]{
			name: "hardware", sing: "hardware", repo: hardware,
			query: inventoryQuery("hardware", "e.name", "e.name"),
			id:    func(h domain.Hardware) int64 { return h.ID }, label: func(h domain.Hardware) string { return h.Name },
		},
	}}
	tags, journal, history := store.NewTagRepo(db), store.NewJournalRepo(db), store.NewChangelogRepo(db)
	b.ReportAllocs()
	for b.Loop() {
		response := httptest.NewRecorder()
		renderDetailExtra(response, httptest.NewRequest(http.MethodGet, "/services/1", nil), db, cat, tags, rels, journal, history,
			"service", 1, "Application", "", "/services/1/edit", "/services", nil, detailExtras{})
		if response.Code != http.StatusOK {
			b.Fatalf("detail status=%d", response.Code)
		}
	}
}
