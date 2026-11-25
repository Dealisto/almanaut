package store

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestImportRejectsInvalidRecordIDs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hosts []domain.Host
	}{
		{"zero", []domain.Host{{ID: 0, Name: "invalid", Type: "vm"}}},
		{"negative", []domain.Host{{ID: -1, Name: "invalid", Type: "vm"}}},
		{"duplicate", []domain.Host{{ID: 2, Name: "one", Type: "vm"}, {ID: 2, Name: "two", Type: "vm"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			repo := NewHostRepo(db)
			id, err := repo.Create(domain.Host{Name: "keep", Type: "vm"})
			if err != nil {
				t.Fatal(err)
			}
			if err := Import(db, Snapshot{Version: 1, Hosts: tc.hosts}); err == nil {
				t.Error("import accepted invalid record IDs")
			}
			hosts, err := repo.List()
			if err != nil || len(hosts) != 1 || hosts[0].ID != id || hosts[0].Name != "keep" {
				t.Fatalf("inventory changed: hosts=%+v err=%v", hosts, err)
			}
		})
	}
}

func TestImportRequiresSupportedVersion(t *testing.T) {
	for _, version := range []int{-1, 0, snapshotVersion + 1} {
		db := newTestDB(t)
		if _, err := NewHostRepo(db).Create(domain.Host{Name: "keep", Type: "vm"}); err != nil {
			t.Fatal(err)
		}
		if err := Import(db, Snapshot{Version: version}); err == nil {
			t.Errorf("import accepted version %d", version)
		}
		if n := countRows(t, db, "SELECT COUNT(*) FROM hosts"); n != 1 {
			t.Errorf("version %d changed inventory", version)
		}
	}
}
