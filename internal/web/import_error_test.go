package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestImportStorageFailuresAreServerErrors(t *testing.T) {
	for _, failure := range []string{"replace", "history"} {
		t.Run(failure, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			hosts := store.NewHostRepo(db)
			id, err := hosts.Create(domain.Host{Name: "keep", Type: "vm"})
			if err != nil {
				t.Fatal(err)
			}
			trigger := `CREATE TRIGGER fail_import BEFORE DELETE ON hosts BEGIN SELECT RAISE(ABORT, 'private database detail'); END`
			if failure == "history" {
				trigger = `CREATE TRIGGER fail_import BEFORE INSERT ON changelog BEGIN SELECT RAISE(ABORT, 'private database detail'); END`
			}
			if _, err := db.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			rec := uploadImport(t, srv, "version: 1\nhosts: []\n", true)
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("failed import status = %d, want 500", rec.Code)
			}
			for _, secret := range []string{"private database detail", "SQL", "clear hosts", "changelog"} {
				if strings.Contains(rec.Body.String(), secret) {
					t.Errorf("failed import leaked storage details %q", secret)
				}
			}
			remaining, err := hosts.List()
			if err != nil || len(remaining) != 1 || remaining[0].ID != id || remaining[0].Name != "keep" {
				t.Fatalf("failed import changed inventory: %+v, %v", remaining, err)
			}
		})
	}
}

func TestImportValidationStillExplainsInvalidRecords(t *testing.T) {
	srv, db := newTestServerDB(t)
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "keep", Type: "vm"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ document, message string }{
		{"version: 999\n", "newer than this almanaut supports"},
		{"version: 1\nhosts:\n  - id: 1\n    name: replacement\n    type: vm\n    ips: [invalid-address]\n", "invalid IP address"},
		{"version: 1\nhosts:\n  - id: 1\n    name: replacement\n    type: vm\ntags:\n  - {id: 1, entity_type: host, entity_id: 1, name: prod}\n  - {id: 2, entity_type: host, entity_id: 1, name: prod}\n", "tag 2 duplicates tag 1"},
	} {
		rec := uploadImport(t, srv, tc.document, true)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.message) {
			t.Errorf("invalid record lost actionable form error %q: status=%d", tc.message, rec.Code)
		}
	}
	remaining, err := store.NewHostRepo(db).List()
	if err != nil || len(remaining) != 1 || remaining[0].Name != "keep" {
		t.Fatalf("invalid import changed inventory: %+v, %v", remaining, err)
	}
}
