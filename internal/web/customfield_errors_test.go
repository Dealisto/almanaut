package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestCreateCustomFieldDistinguishesDuplicateAndStorageFailure(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		name := "duplicate"
		if failWrite {
			name = "storage failure"
		}
		t.Run(name, func(t *testing.T) {
			handler, db := newTestServerDB(t)
			form := url.Values{"entity_type": {"host"}, "name": {"asset_tag"}, "label": {"Asset tag"}, "kind": {"text"}}
			if rec := postForm(t, handler, "/custom-fields", form); rec.Code != http.StatusSeeOther {
				t.Fatalf("create original: status=%d body=%s", rec.Code, rec.Body)
			}
			if failWrite {
				if _, err := db.Exec(`CREATE TRIGGER fail_custom_field_insert BEFORE INSERT ON custom_field_definitions BEGIN SELECT RAISE(ABORT, 'internal write failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			form.Set("label", "Changed label")
			rec := postForm(t, handler, "/custom-fields", form)
			if failWrite {
				if rec.Code != http.StatusInternalServerError || rec.Body.String() != "internal server error\n" {
					t.Errorf("storage failure must return generic 500: status=%d body=%s", rec.Code, rec.Body)
				}
			} else if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "already exists for host") {
				t.Errorf("duplicate must retain actionable form error: status=%d body=%s", rec.Code, rec.Body)
			}
			defs, err := store.NewCustomFieldRepo(db).ListDefs("host")
			if err != nil || len(defs) != 1 || defs[0].Label != "Asset tag" {
				t.Fatalf("failed creation changed existing definition: %+v, %v", defs, err)
			}
		})
	}
}

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
