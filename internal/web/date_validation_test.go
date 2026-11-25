package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestAPIAcceptedPaddedExpiryAppearsInChecks(t *testing.T) {
	srv, db := newTestServerDB(t)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/certificates",
		strings.NewReader(`{"subject":"example.test","expires_on":" 2026-01-01 "}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create certificate = %d: %s", rec.Code, rec.Body)
	}
	certs, err := store.NewCertificateRepo(db).List()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	if got := domain.ExpiringSoon(certs, now, 30); len(got) != 1 {
		t.Fatalf("accepted expired certificate missing from expiry results: %+v", certs)
	}
}
