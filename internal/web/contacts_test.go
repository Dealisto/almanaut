package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestContactCRUDAndRelationship(t *testing.T) {
	srv, _ := newTestServerDB(t)

	// Create a contact.
	if rec := postForm(t, srv, "/contacts", url.Values{"name": {"Ada"}, "email": {"ada@x.io"}, "role": {"admin"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create contact = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	// Create a host to link it to.
	if rec := postForm(t, srv, "/hosts", url.Values{"name": {"box"}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create host = %d", rec.Code)
	}
	// Link: host administered by contact (both id 1).
	rec := postForm(t, srv, "/relationships", url.Values{
		"from": {"host:1"}, "kind": {"administered by"}, "to": {"contact:1"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create relationship = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	// The contact appears on the host detail page.
	req := httptest.NewRequest(http.MethodGet, "/hosts/1", nil)
	drec := httptest.NewRecorder()
	srv.ServeHTTP(drec, req)
	if drec.Code != http.StatusOK || !strings.Contains(drec.Body.String(), "administered by") {
		t.Fatalf("host detail should show the contact relationship; got %d", drec.Code)
	}
}
