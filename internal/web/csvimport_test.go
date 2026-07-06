package web

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// hostResourceForTest builds a resource[domain.Host] wired to a fresh,
// migrated on-disk SQLite db, plus a handlerDeps with just the fields
// importCSV/createEntityTx/updateEntityTx touch (db, changelog).
func hostResourceForTest(t *testing.T) (resource[domain.Host], handlerDeps) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	hosts := store.NewHostRepo(db)
	rs := resource[domain.Host]{
		name: "hosts", sing: "host", title: "Hosts", heading: "Host",
		repo:  hosts,
		parse: parseHost,
		label: func(h domain.Host) string { return h.Name },
		id:    func(h domain.Host) int64 { return h.ID },
		setID: func(h *domain.Host, id int64) { h.ID = id },
		notes: func(h domain.Host) string { return h.Notes },
		fields: func(h domain.Host) []fieldRow {
			return []fieldRow{
				{"Type", h.Type}, {"OS", h.OS}, {"CPU", h.CPU}, {"RAM", h.RAM},
				{"Disk", h.Disk}, {"Status", h.Status}, {"IPs", strings.Join(h.IPs, ", ")},
			}
		},
		search: func(h domain.Host) []string {
			return []string{h.Name, h.OS, h.CPU, h.RAM, h.Disk, h.Status, h.Notes, strings.Join(h.IPs, " ")}
		},
		newItem:  domain.Host{Type: "physical"},
		listTmpl: "hosts.html", formTmpl: "host_form.html",
	}

	deps := handlerDeps{db: db, changelog: store.NewChangelogRepo(db)}
	return rs, deps
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func TestCsvFieldsHost(t *testing.T) {
	got := csvFields(domain.Host{})
	for _, want := range []string{"id", "name", "type", "os", "cpu", "ram", "disk", "status", "ips", "notes"} {
		if !got[want] {
			t.Errorf("csvFields(Host) missing %q", want)
		}
	}
	if got["bogus"] {
		t.Errorf("csvFields(Host) should not contain bogus")
	}
}

func TestImportCSVCreatesRows(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	csv := "name,type,ips\nweb,physical,10.0.0.1\ndb,vm,10.0.0.2\n"
	created, updated, rowErrs, err := rs.importCSV(deps, strings.NewReader(csv), "tester")
	if err != nil {
		t.Fatalf("importCSV: %v", err)
	}
	if len(rowErrs) != 0 {
		t.Fatalf("unexpected row errors: %v", rowErrs)
	}
	if created != 2 || updated != 0 {
		t.Fatalf("got created=%d updated=%d, want 2/0", created, updated)
	}
	hosts, _ := rs.repo.List()
	if len(hosts) != 2 {
		t.Fatalf("want 2 hosts persisted, got %d", len(hosts))
	}
}

func TestImportCSVUpdatesByID(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	id, err := rs.createEntity(deps, domain.Host{Name: "old", Type: "physical"}, "seed")
	if err != nil {
		t.Fatal(err)
	}
	csv := "id,name,type\n" + itoa(id) + ",new,vm\n"
	created, updated, rowErrs, err := rs.importCSV(deps, strings.NewReader(csv), "tester")
	if err != nil || len(rowErrs) != 0 {
		t.Fatalf("importCSV err=%v rowErrs=%v", err, rowErrs)
	}
	if created != 0 || updated != 1 {
		t.Fatalf("got created=%d updated=%d, want 0/1", created, updated)
	}
	h, _ := rs.repo.Get(id)
	if h.Name != "new" || h.Type != "vm" {
		t.Fatalf("row not updated: %+v", h)
	}
}

func TestImportCSVUnknownHeaderRejected(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	_, _, rowErrs, err := rs.importCSV(deps, strings.NewReader("name,bogus\nx,y\n"), "t")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rowErrs) == 0 || !strings.Contains(rowErrs[0], "bogus") {
		t.Fatalf("want unknown-column error, got %v", rowErrs)
	}
}

func TestImportCSVInvalidRowAbortsBatch(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	// Second row has an empty name -> Host.Validate fails. Nothing must persist.
	csv := "name,type\nweb,physical\n,vm\n"
	created, updated, rowErrs, err := rs.importCSV(deps, strings.NewReader(csv), "t")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if created != 0 || updated != 0 {
		t.Fatalf("expected no writes, got created=%d updated=%d", created, updated)
	}
	if len(rowErrs) != 1 || !strings.Contains(rowErrs[0], "row 3") {
		t.Fatalf("want one row-3 error, got %v", rowErrs)
	}
	if hosts, _ := rs.repo.List(); len(hosts) != 0 {
		t.Fatalf("batch should have aborted, but %d hosts persisted", len(hosts))
	}
}

func TestImportCSVUnknownIDIsRowError(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	_, _, rowErrs, err := rs.importCSV(deps, strings.NewReader("id,name,type\n9999,x,vm\n"), "t")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rowErrs) != 1 || !strings.Contains(rowErrs[0], "9999") {
		t.Fatalf("want not-found row error, got %v", rowErrs)
	}
}
