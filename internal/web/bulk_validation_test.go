package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestBulkRejectsInvalidSelectionBeforeMutation(t *testing.T) {
	for _, invalid := range []string{"bad", "", "0", "-1", "9223372036854775808"} {
		t.Run(invalid, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			if rec := postForm(t, srv, "/hosts", url.Values{"name": {"keep"}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
				t.Fatalf("create status = %d", rec.Code)
			}
			rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {"delete"}, "ids": {"1", invalid}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("bulk status = %d, want 400", rec.Code)
			}
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("invalid selection changed inventory: count=%d err=%v", count, err)
			}
		})
	}
}

func TestBulkDeleteDeduplicatesSelection(t *testing.T) {
	srv, db := newTestServerDB(t)
	for _, name := range []string{"a", "b"} {
		if rec := postForm(t, srv, "/hosts", url.Values{"name": {name}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
			t.Fatalf("create status = %d", rec.Code)
		}
	}
	rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {"delete"}, "ids": {"1", "01", " 1 ", "2", "2"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("bulk status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var remaining, events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM changelog WHERE action='delete' AND entity_type='host'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 || events != 2 {
		t.Fatalf("remaining=%d delete events=%d, want 0 and 2", remaining, events)
	}
}

func TestBulkErrorsPreserveInventoryAndHistory(t *testing.T) {
	for _, tc := range []struct {
		name, field, value, trigger, message string
		ids                                  []string
		status                               int
	}{
		{name: "invalid value", field: "name", value: " ", ids: []string{"1", "2"}, status: http.StatusBadRequest, message: "name is required"},
		{name: "unknown catalog type", field: "type", value: "missing_type", ids: []string{"1", "2"}, status: http.StatusBadRequest, message: "host type"},
		{name: "missing entity", field: "name", value: "changed", ids: []string{"1", "999"}, status: http.StatusNotFound, message: "host not found"},
		{name: "storage failure", field: "name", value: "changed", ids: []string{"1", "2"}, status: http.StatusInternalServerError, message: "internal server error", trigger: `CREATE TRIGGER fail_bulk BEFORE UPDATE ON hosts WHEN OLD.id=2 BEGIN SELECT RAISE(ABORT, 'private storage failure'); END`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			for _, name := range []string{"a", "b"} {
				if rec := postForm(t, srv, "/hosts", url.Values{"name": {name}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
					t.Fatalf("create status = %d", rec.Code)
				}
			}
			if tc.trigger != "" {
				if _, err := db.Exec(tc.trigger); err != nil {
					t.Fatal(err)
				}
			}
			rec := postForm(t, srv, "/hosts/bulk", url.Values{"action": {"set-field"}, "ids": tc.ids, "field": {tc.field}, "value": {tc.value}})
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.message) {
				t.Fatalf("status=%d body=%q, want %d containing %q", rec.Code, rec.Body.String(), tc.status, tc.message)
			}
			if strings.Contains(rec.Body.String(), "private storage failure") {
				t.Fatal("response exposed storage internals")
			}
			var unchanged, events int
			if err := db.QueryRow(`SELECT COUNT(*) FROM hosts WHERE name IN ('a','b') AND type='physical'`).Scan(&unchanged); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM changelog WHERE action='update' AND entity_type='host'`).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if unchanged != 2 || events != 0 {
				t.Fatalf("failed batch changed inventory/history: unchanged=%d events=%d", unchanged, events)
			}
		})
	}
}
