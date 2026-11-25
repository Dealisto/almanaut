package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestCustomFieldAPIRejectsCompoundValuesWithoutWrites(t *testing.T) {
	for _, kind := range domain.CustomFieldKinds {
		for _, value := range []string{`{"value":true}`, `["ABC","DEF"]`, `{}`, `[]`} {
			t.Run(string(kind)+value, func(t *testing.T) {
				srv, db := newTestServerDB(t)
				fields := store.NewCustomFieldRepo(db)
				defID, err := fields.CreateDef(domain.CustomFieldDef{EntityType: "host", Name: "extra", Label: "Extra", Kind: kind})
				if err != nil {
					t.Fatal(err)
				}
				hosts := store.NewHostRepo(db)
				id, err := hosts.Create(domain.Host{Name: "Existing", Type: "physical"})
				if err != nil {
					t.Fatal(err)
				}
				original := map[domain.CustomFieldKind]string{domain.KindText: "Original", domain.KindNumber: "42", domain.KindBool: "true", domain.KindDate: "2026-11-25"}[kind]
				if err := fields.SetForEntity("host", id, map[int64]string{defID: original}); err != nil {
					t.Fatal(err)
				}
				for _, method := range []string{http.MethodPost, http.MethodPut} {
					path := "/api/hosts"
					if method == http.MethodPut {
						path = fmt.Sprintf("%s/%d", path, id)
					}
					rec := httptest.NewRecorder()
					body := fmt.Sprintf(`{"name":"Changed","type":"physical","custom_fields":{"extra":%s}}`, value)
					srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
					if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Extra") {
						t.Fatalf("%s: %d %s", method, rec.Code, rec.Body)
					}
				}
				got, err := hosts.List()
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != 1 || got[0].Name != "Existing" {
					t.Fatalf("invalid request changed inventory: %+v", got)
				}
				values, err := fields.ListForEntity("host", id)
				if err != nil {
					t.Fatal(err)
				}
				if len(values) != 1 || values[0].Value != original {
					t.Fatalf("invalid request changed custom field: %+v", values)
				}
			})
		}
	}
}

func TestCustomFieldAPIScalarCompatibility(t *testing.T) {
	srv, db := newTestServerDB(t)
	fields := store.NewCustomFieldRepo(db)
	for _, def := range []domain.CustomFieldDef{
		{EntityType: "host", Name: "text", Label: "Text", Kind: domain.KindText},
		{EntityType: "host", Name: "number", Label: "Number", Kind: domain.KindNumber},
		{EntityType: "host", Name: "flag", Label: "Flag", Kind: domain.KindBool},
		{EntityType: "host", Name: "date", Label: "Date", Kind: domain.KindDate},
	} {
		if _, err := fields.CreateDef(def); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ input, want string }{
		{`{"text":"hello","number":12.5,"flag":true,"date":"2026-11-25"}`, `"custom_fields":{"date":"2026-11-25","flag":true,"number":12.5,"text":"hello"}`},
		{`{"text":123,"number":"12.5","flag":"yes"}`, `"custom_fields":{"flag":true,"number":12.5,"text":"123"}`},
		{`{"flag":1e0}`, `"custom_fields":{"flag":true}`},
		{`{"text":null,"number":null,"flag":null,"date":null}`, `"custom_fields":{"flag":false}`},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/hosts", strings.NewReader(`{"name":"Valid","type":"physical","custom_fields":`+tc.input+`}`)))
		if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("input %s: %d %s", tc.input, rec.Code, rec.Body)
		}
	}
}
