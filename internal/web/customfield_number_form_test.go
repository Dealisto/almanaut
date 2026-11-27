package web

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
)

func TestCustomNumberFormsPreserveRepresentations(t *testing.T) {
	for _, tc := range []struct{ value, inputType string }{
		{"+12", "text"}, {"1_000", "text"}, {"0x1p2", "text"}, {"1.", "text"}, {".5", "text"}, {"01", "text"},
		{"12.00", "number"}, {"1e2", "number"}, {"-12.5", "number"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			post := func(path string, form url.Values, status int) string {
				t.Helper()
				rec := postForm(t, srv, path, form)
				if rec.Code != status {
					t.Fatalf("POST %s = %d: %s", path, rec.Code, rec.Body)
				}
				return rec.Body.String()
			}
			post("/custom-fields", url.Values{"name": {"amount"}, "label": {"Amount"}, "kind": {"number"}, "entity_type": {"host"}}, http.StatusSeeOther)
			form := url.Values{"name": {"Host"}, "type": {"physical"}, "cf_amount": {tc.value}}
			post("/hosts", form, http.StatusSeeOther)
			assertInput := func(body string) {
				t.Helper()
				input := `type="number" step="any"`
				if tc.inputType == "text" {
					input = `type="text" inputmode="decimal"`
				}
				input += ` name="cf_amount" value="` + tc.value + `"`
				if !strings.Contains(html.UnescapeString(body), input) {
					t.Fatalf("missing preserving number input %q", input)
				}
			}
			assertInput(getPage(t, srv, "/hosts/1/edit"))
			form.Set("name", "")
			assertInput(post("/hosts/1", form, http.StatusOK))
			form.Set("name", "Host")
			form.Set("notes", "Edited notes")
			post("/hosts/1", form, http.StatusSeeOther)
			values, err := store.NewCustomFieldRepo(db).ListForEntity("host", 1)
			if err != nil || len(values) != 1 || values[0].Value != tc.value {
				t.Fatalf("number changed on retry: %+v %v", values, err)
			}
		})
	}
}

func TestCustomNumberInvalidDraftRemainsCorrectable(t *testing.T) {
	for _, raw := range []string{"not a number", "1e999", "NaN", `bad"><script>bad</script>`} {
		t.Run(raw, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			if rec := postForm(t, srv, "/custom-fields", url.Values{"name": {"amount"}, "label": {"Amount"}, "kind": {"number"}, "entity_type": {"host"}}); rec.Code != http.StatusSeeOther {
				t.Fatal(rec.Code)
			}
			form := url.Values{"name": {"Host"}, "type": {"physical"}, "cf_amount": {"12"}}
			if rec := postForm(t, srv, "/hosts", form); rec.Code != http.StatusSeeOther {
				t.Fatal(rec.Code)
			}
			form.Set("cf_amount", raw)
			for attempt := 0; attempt < 2; attempt++ {
				rec := postForm(t, srv, "/hosts/1", form)
				expected := `type="text" inputmode="decimal" name="cf_amount" value="` + html.EscapeString(raw) + `"`
				if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), expected) || strings.Contains(rec.Body.String(), "<script>bad</script>") {
					t.Fatalf("invalid number lost draft: %d %s", rec.Code, rec.Body)
				}
				values, err := store.NewCustomFieldRepo(db).ListForEntity("host", 1)
				if err != nil || len(values) != 1 || values[0].Value != "12" {
					t.Fatalf("invalid draft changed stored number: %+v %v", values, err)
				}
			}
		})
	}
}
