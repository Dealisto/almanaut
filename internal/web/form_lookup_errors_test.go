package web

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestFormsRejectFailedOptionLookups(t *testing.T) {
	for _, tc := range []struct{ path, table string }{
		{"/nics/new", "hosts"},
		{"/ports/new", "nics"},
		{"/ports/new", "ports"},
		{"/ports/new", "hosts"},
		{"/ports/new", "hardware"},
		{"/networks/new", "vlans"},
		{"/hardware/new", "racks"},
		{"/locations/new", "sites"},
		{"/racks/new", "locations"},
		{"/reservations/new", "networks"},
	} {
		t.Run(tc.path+"/"+tc.table, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			if _, err := db.Exec("DROP TABLE " + tc.table); err != nil {
				t.Fatal(err)
			}
			rec := getJSONResp(t, srv, tc.path)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status %d, want 500", rec.Code)
			}
			if strings.Contains(rec.Body.String(), `<form class="stack"`) {
				t.Fatal("failed lookup exposed an editable form")
			}
		})
	}
}

func TestHardwareEditRejectsUnreadableRackOptions(t *testing.T) {
	srv, db := newTestServerDB(t)
	rackID, err := store.NewRackRepo(db).Create(domain.Rack{Name: "Rack A", UHeight: 42})
	if err != nil {
		t.Fatal(err)
	}
	hw := store.NewHardwareRepo(db)
	id, err := hw.Create(domain.Hardware{Name: "Switch", RackID: rackID, RackPosition: 1, UHeight: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE racks SET u_height='unreadable' WHERE id=?`, rackID); err != nil {
		t.Fatal(err)
	}
	rec := getJSONResp(t, srv, fmt.Sprintf("/hardware/%d/edit", id))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `name="rack_id"`) {
		t.Fatal("failed lookup exposed an empty rack selector")
	}
	got, err := hw.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.RackID != rackID {
		t.Fatalf("rack relation changed: %d, want %d", got.RackID, rackID)
	}
}
