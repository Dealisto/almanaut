package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestCustomFieldNumbersRoundTripWithoutRounding(t *testing.T) {
	srv, db := newTestServerDB(t)
	fields := store.NewCustomFieldRepo(db)
	for _, def := range []domain.CustomFieldDef{
		{EntityType: "host", Name: "number", Label: "Number", Kind: domain.KindNumber},
		{EntityType: "host", Name: "text", Label: "Text", Kind: domain.KindText},
	} {
		if _, err := fields.CreateDef(def); err != nil {
			t.Fatal(err)
		}
	}
	for _, number := range []string{"9007199254740993", "0.12345678901234567890123456789", "1e-400"} {
		t.Run(number, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/hosts", strings.NewReader(`{"name":"Exact","type":"physical","custom_fields":{"number":`+number+`,"text":`+number+`}}`)))
			if rec.Code != http.StatusCreated {
				t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
			}
			var host domain.Host
			if err := json.Unmarshal(rec.Body.Bytes(), &host); err != nil {
				t.Fatal(err)
			}
			check := func(body []byte) {
				t.Helper()
				var got struct {
					CustomFields map[string]json.RawMessage `json:"custom_fields"`
				}
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatal(err)
				}
				if string(got.CustomFields["number"]) != number || string(got.CustomFields["text"]) != `"`+number+`"` {
					t.Fatalf("numeric value rounded: %s", body)
				}
			}
			check(rec.Body.Bytes())
			values, err := fields.ListForEntity("host", host.ID)
			if err != nil || len(values) != 2 {
				t.Fatalf("stored values=%v err=%v", values, err)
			}
			for _, value := range values {
				if value.Value != number {
					t.Errorf("stored %s=%q, want %q", value.Name, value.Value, number)
				}
			}
			get := httptest.NewRecorder()
			srv.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/hosts/"+itoa(host.ID), nil))
			check(get.Body.Bytes())
		})
	}
}

func TestCustomFieldNumberResponseAcceptsLegacyNotation(t *testing.T) {
	for _, value := range []string{"+1", "01", ".5", "0x1p2"} {
		t.Run(value, func(t *testing.T) {
			body, err := mergeCustomFields(domain.Host{}, []domain.CustomFieldValue{{Name: "number", Kind: domain.KindNumber, Value: value}})
			if err != nil || !json.Valid(body) {
				t.Fatalf("legacy notation broke JSON: body=%s err=%v", body, err)
			}
		})
	}
}
