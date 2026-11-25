package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestImportRejectsAmbiguousSnapshotsWithoutChangingInventory(t *testing.T) {
	for _, tc := range []struct{ name, document string }{
		{"empty", ""},
		{"comments", "# Not an inventory\n"},
		{"null", "null\n"},
		{"empty mapping", "{}\n"},
		{"unrelated file", "services:\n  proxy:\n    image: nginx\n"},
		{"unknown field", "version: 1\nhostz: []\n"},
		{"unknown nested field", "version: 1\nhosts:\n  - id: 1\n    name: new\n    type: vm\n    ipz: [10.0.0.1]\n"},
		{"missing version", "hosts: []\n"},
		{"zero version", "version: 0\nhosts: []\n"},
		{"negative version", "version: -1\nhosts: []\n"},
		{"multiple documents", "version: 1\nhosts: []\n---\nversion: 1\nhosts: []\n"},
		{"trailing null document", "version: 1\nhosts: []\n---\nnull\n"},
		{"trailing malformed document", "version: 1\nhosts: []\n---\n[invalid\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			hosts := store.NewHostRepo(db)
			id, err := hosts.Create(domain.Host{Name: "keep", Type: "vm"})
			if err != nil {
				t.Fatal(err)
			}
			rec := uploadImport(t, srv, tc.document, true)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "invalid YAML") {
				t.Errorf("expected import error page, status=%d body=%s", rec.Code, rec.Body.String())
			}
			got, err := hosts.List()
			if err != nil || len(got) != 1 || got[0].ID != id || got[0].Name != "keep" {
				t.Fatalf("invalid upload changed inventory: hosts=%+v err=%v", got, err)
			}
			var events int
			if err := db.QueryRow(`SELECT COUNT(*) FROM changelog`).Scan(&events); err != nil || events != 0 {
				t.Fatalf("invalid upload changed history: count=%d err=%v", events, err)
			}
		})
	}
}

func TestImportAcceptsExplicitEmptyInventory(t *testing.T) {
	srv, db := newTestServerDB(t)
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "remove", Type: "vm"}); err != nil {
		t.Fatal(err)
	}
	rec := uploadImport(t, srv, "version: 1\nhosts: []\n", true)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	hosts, err := store.NewHostRepo(db).List()
	if err != nil || len(hosts) != 0 {
		t.Fatalf("empty inventory not restored: hosts=%+v err=%v", hosts, err)
	}
}
