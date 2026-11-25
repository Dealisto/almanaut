package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestCustomFieldAPIDatabaseFailuresAreServerErrors(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			handler, db := newTestServerDB(t)
			hosts := store.NewHostRepo(db)
			if _, err := hosts.Create(domain.Host{Name: "Original", Type: "vm"}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`DROP TABLE custom_field_definitions`); err != nil {
				t.Fatal(err)
			}
			path := "/api/hosts"
			if method == http.MethodPut {
				path += "/1"
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(`{"name":"Changed","type":"vm","custom_fields":{"extra":"value"}}`)))
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body)
			}
			for _, detail := range []string{"SQL", "custom_field_definitions", "no such table"} {
				if strings.Contains(rec.Body.String(), detail) {
					t.Errorf("response exposes %q: %s", detail, rec.Body)
				}
			}
			rows, err := hosts.List()
			if err != nil || len(rows) != 1 || rows[0].Name != "Original" {
				t.Fatalf("failed lookup changed inventory: %+v, %v", rows, err)
			}
		})
	}
}
