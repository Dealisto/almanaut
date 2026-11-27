package web

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDateFormsPreserveAPIAcceptedValues(t *testing.T) {
	for _, tc := range []struct{ route, field, label string }{
		{"/certificates", "expires_on", "subject"},
		{"/backups", "last_run", "source"},
		{"/hardware", "purchase_date", "name"},
		{"/hardware", "warranty_end", "name"},
		{"/subscriptions", "renewal_date", "name"},
	} {
		for _, value := range []string{"2030-02-28", " 2030-02-28 ", "0000-01-01"} {
			t.Run(tc.field+"/"+value, func(t *testing.T) {
				srv, _ := newTestServerDB(t)
				payload, err := json.Marshal(map[string]string{tc.label: "Original", tc.field: value})
				if err != nil {
					t.Fatal(err)
				}
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api"+tc.route, strings.NewReader(string(payload))))
				if rec.Code != http.StatusCreated {
					t.Fatalf("API create = %d: %s", rec.Code, rec.Body)
				}
				path := tc.route + "/1"
				body := getPage(t, srv, path+"/edit")
				inputType := "text"
				if value == "2030-02-28" {
					inputType = "date"
				}
				assertDateInput(t, body, tc.field, value, inputType)
				form := url.Values{tc.label: {"Original"}, tc.field: {value}, "notes": {"Changed notes"}}
				if rec := postForm(t, srv, path, form); rec.Code != http.StatusSeeOther {
					t.Fatalf("save notes = %d: %s", rec.Code, rec.Body)
				}
				assertStored := func(wantDate string) {
					t.Helper()
					rec := httptest.NewRecorder()
					srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api"+path, nil))
					var item map[string]any
					if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil || rec.Code != http.StatusOK {
						t.Fatalf("read saved item = %d: %s, %v", rec.Code, rec.Body, err)
					}
					if item[tc.field] != wantDate || item["notes"] != "Changed notes" {
						t.Fatalf("date or notes changed unexpectedly: %+v", item)
					}
				}
				assertStored(strings.TrimSpace(value))
				invalid := `bad"><script>invalid</script>`
				form.Set(tc.field, invalid)
				for attempt := 0; attempt < 2; attempt++ {
					rec := postForm(t, srv, path, form)
					if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `role="alert"`) {
						t.Fatalf("invalid date response = %d: %s", rec.Code, rec.Body)
					}
					assertDateInput(t, rec.Body.String(), tc.field, invalid, "text")
					if strings.Contains(rec.Body.String(), "<script>invalid</script>") {
						t.Fatal("unescaped date draft")
					}
					assertStored(strings.TrimSpace(value))
				}
			})
		}
	}
}

func assertDateInput(t *testing.T, body, field, value, inputType string) {
	t.Helper()
	want := `type="` + inputType + `" name="` + field + `" value="` + html.EscapeString(value) + `"`
	if !strings.Contains(body, want) {
		t.Fatalf("missing lossless date input %s", want)
	}
}
