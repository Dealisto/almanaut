package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/almanaut/almanaut/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
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
	return New(store.NewHostRepo(db))
}

func TestCreateAndListHost(t *testing.T) {
	srv := newTestServer(t)

	// Create via form POST
	form := url.Values{"name": {"web01"}, "type": {"vm"}, "os": {"Debian 12"}, "ips": {"10.0.0.5"}}
	req := httptest.NewRequest(http.MethodPost, "/hosts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /hosts status = %d, want 303", rec.Code)
	}

	// List shows the new host
	req = httptest.NewRequest(http.MethodGet, "/hosts", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /hosts status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "web01") {
		t.Errorf("GET /hosts body does not contain created host")
	}
}

func TestCreateHostInvalidShowsError(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"name": {""}, "type": {"vm"}}
	req := httptest.NewRequest(http.MethodPost, "/hosts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid POST status = %d, want 200 (re-render with error)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "name is required") {
		t.Errorf("invalid POST body missing validation error")
	}
}
