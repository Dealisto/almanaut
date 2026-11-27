package web

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestCustomFieldBooleanNumbersAreExact(t *testing.T) {
	srv, db := newTestServerDB(t)
	fields := store.NewCustomFieldRepo(db)
	defID, err := fields.CreateDef(domain.CustomFieldDef{EntityType: "host", Name: "flag", Label: "Flag", Kind: domain.KindBool})
	if err != nil {
		t.Fatal(err)
	}
	hosts := store.NewHostRepo(db)
	id, err := hosts.Create(domain.Host{Name: "Keep", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	if err := fields.SetForEntity("host", id, map[int64]string{defID: "true"}); err != nil {
		t.Fatal(err)
	}
	for _, number := range []string{"1.00000000000000001", "0.99999999999999999", "1e-1000", "-1e-1000", "2", "1e1000"} {
		t.Run(number, func(t *testing.T) {
			for _, method := range []string{http.MethodPost, http.MethodPut} {
				path := "/api/hosts"
				if method == http.MethodPut {
					path += "/" + itoa(id)
				}
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(`{"name":"Changed","type":"physical","custom_fields":{"flag":`+number+`}}`)))
				if rec.Code != http.StatusBadRequest {
					t.Errorf("%s %s: status=%d body=%s", method, number, rec.Code, rec.Body)
				}
			}
		})
	}
	items, err := hosts.List()
	if err != nil || len(items) != 1 || items[0].Name != "Keep" {
		t.Fatalf("invalid boolean changed inventory: %+v, %v", items, err)
	}
	values, err := fields.ListForEntity("host", id)
	if err != nil || len(values) != 1 || values[0].Value != "true" {
		t.Fatalf("invalid boolean changed field: %+v, %v", values, err)
	}
}

func TestCustomFieldBooleanLongNumbers(t *testing.T) {
	srv, db := newTestServerDB(t)
	if _, err := store.NewCustomFieldRepo(db).CreateDef(domain.CustomFieldDef{EntityType: "host", Name: "flag", Label: "Flag", Kind: domain.KindBool}); err != nil {
		t.Fatal(err)
	}
	const places = 100000
	zeros := strings.Repeat("0", places)
	for _, tc := range []struct {
		name, number, want string
	}{
		{"negative zero", "-0.0", "false"},
		{"zero with enormous exponent", "-0e-" + strings.Repeat("9", places), "false"},
		{"integer power", "1" + zeros + "e-" + strconv.Itoa(places), "true"},
		{"fractional power", "0." + zeros + "1e" + strconv.Itoa(places+1), "true"},
		{"exponent leading zeros", "0.1e+" + zeros + "1", "true"},
		{"near one", "1." + zeros + "1", ""},
		{"tiny nonzero", "0." + zeros + "1", ""},
		{"enormous nonzero exponent", "1e" + strings.Repeat("9", places), ""},
		{"negative one", "-1.0E0", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/hosts", strings.NewReader(`{"name":"Boolean","type":"physical","custom_fields":{"flag":`+tc.number+`}}`)))
			if tc.want == "" {
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("invalid boolean status=%d body=%s", rec.Code, rec.Body)
				}
				return
			}
			if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"flag":`+tc.want) {
				t.Fatalf("valid boolean status=%d body=%s", rec.Code, rec.Body)
			}
		})
	}
}

func TestCustomFieldBooleanNumericRepresentations(t *testing.T) {
	srv, db := newTestServerDB(t)
	if _, err := store.NewCustomFieldRepo(db).CreateDef(domain.CustomFieldDef{EntityType: "host", Name: "flag", Label: "Flag", Kind: domain.KindBool}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ number, want string }{{"0", "false"}, {"0.0", "false"}, {"0e12", "false"}, {"1.0", "true"}, {"1e0", "true"}, {"10e-1", "true"}} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/hosts", strings.NewReader(`{"name":"Valid","type":"physical","custom_fields":{"flag":`+tc.number+`}}`)))
		if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"flag":`+tc.want) {
			t.Errorf("%s: %d %s", tc.number, rec.Code, rec.Body)
		}
	}
}
