package web

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestActivityLinksOnlyResolveReferencedEntities(t *testing.T) {
	_, db := newTestServerDB(t)
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "current host", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewHardwareRepo(db).Create(domain.Hardware{Name: "current switch", Kind: "switch"}); err != nil {
		t.Fatal(err)
	}
	cat := entityCatalog{resources: []mountable{
		resource[domain.Host]{name: "hosts", sing: "host", repo: store.NewHostRepo(db)},
		resource[domain.Hardware]{name: "hardware", sing: "hardware", repo: store.NewHardwareRepo(db)},
		resource[domain.Service]{name: "services", sing: "service", repo: store.NewServiceRepo(db)},
	}}
	// No service is referenced. Its unavailable table must not prevent linking
	// a bounded feed of host/hardware records or force unrelated inventory reads.
	if _, err := db.Exec(`DROP TABLE services`); err != nil {
		t.Fatal(err)
	}
	events := []store.ChangeEvent{
		{EntityType: "host", EntityID: 1, Label: "historical host", Action: domain.ActionUpdate},
		{EntityType: "hardware", EntityID: 1, Label: "historical switch", Action: domain.ActionCreate},
		{EntityType: "host", EntityID: 999, Label: "removed host", Action: domain.ActionUpdate},
		{EntityType: "host", EntityID: 1, Label: "deleted identity", Action: domain.ActionDelete},
		{EntityType: "host", EntityID: 1, Label: "import event", Action: domain.ActionImport},
		{EntityType: "unknown", EntityID: 1, Label: "unknown", Action: domain.ActionUpdate},
	}
	rows, err := buildActivityRows(cat, store.NewChangelogRepo(db), events)
	if err != nil || len(rows) != len(events) {
		t.Fatalf("rows=%v error=%v", rows, err)
	}
	paths := []string{"/hosts/1", "/hardware/1", "", "", "", ""}
	for i, row := range rows {
		if row.Path != paths[i] || row.Label != events[i].Label || row.Action != events[i].Action {
			t.Errorf("row %d changed frozen fields or link: %+v", i, row)
		}
	}
	if _, err := db.Exec(`DROP TABLE hosts`); err != nil {
		t.Fatal(err)
	}
	if _, err := buildActivityRows(cat, store.NewChangelogRepo(db), events); err == nil {
		t.Fatal("referenced table failure was hidden as deleted entities")
	}
}
