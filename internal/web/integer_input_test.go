package web

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestIntegerErrorFormsKeepSubmittedValues(t *testing.T) {
	for _, tc := range []struct {
		base, field string
		form        url.Values
	}{
		{"hosts", "u_height", url.Values{"name": {"Original"}, "type": {"physical"}, "u_height": {"2"}}},
		{"hosts", "rack_position", url.Values{"name": {"Original"}, "type": {"physical"}, "rack_position": {"2"}}},
		{"hardware", "u_height", url.Values{"name": {"Original"}, "u_height": {"2"}}},
		{"hardware", "rack_position", url.Values{"name": {"Original"}, "rack_position": {"2"}}},
		{"racks", "u_height", url.Values{"name": {"Original"}, "u_height": {"2"}}},
		{"vlans", "vid", url.Values{"name": {"Original"}, "vid": {"2"}}},
	} {
		for _, edit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%s/edit=%t", tc.base, tc.field, edit), func(t *testing.T) {
				srv, db := newTestServerDB(t)
				form := url.Values{}
				for key, values := range tc.form {
					form[key] = append([]string(nil), values...)
				}
				if rec := postForm(t, srv, "/"+tc.base, form); rec.Code != http.StatusSeeOther {
					t.Fatalf("seed = %d %s", rec.Code, rec.Body)
				}
				assertInput := func(body, want string) {
					t.Helper()
					tag := regexp.MustCompile(`<input[^>]*name="` + tc.field + `"[^>]*>`).FindString(body)
					if !strings.Contains(html.UnescapeString(tag), `value="`+want+`"`) {
						t.Fatalf("%s draft should be %q: %s", tc.field, want, tag)
					}
				}
				assertInput(getPage(t, srv, "/"+tc.base+"/1/edit?"+tc.field+"=999"), "2")
				path := "/" + tc.base
				if edit {
					path += "/1"
				}
				form.Set(tc.field, "1e1")
				form.Set("notes", "Keep the integer draft")
				for attempt := 0; attempt < 2; attempt++ {
					rec := postForm(t, srv, path+"?"+tc.field+"=999", form)
					if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `role="alert"`) {
						t.Fatalf("integer error = %d %s", rec.Code, rec.Body)
					}
					assertInput(rec.Body.String(), "1e1")
					var count, value int
					if err := db.QueryRow("SELECT count(*) FROM " + tc.base).Scan(&count); err != nil {
						t.Fatal(err)
					}
					if err := db.QueryRow("SELECT " + tc.field + " FROM " + tc.base + " WHERE id=1").Scan(&value); err != nil {
						t.Fatal(err)
					}
					if count != 1 || value != 2 {
						t.Fatalf("invalid retry changed inventory: count=%d value=%d", count, value)
					}
				}
				form.Set(tc.field, "10")
				if rec := postForm(t, srv, path, form); rec.Code != http.StatusSeeOther {
					t.Fatalf("correct integer = %d %s", rec.Code, rec.Body)
				}
				id := 2
				if edit {
					id = 1
				}
				var value int
				var notes string
				if err := db.QueryRow("SELECT "+tc.field+", notes FROM "+tc.base+" WHERE id=?", id).Scan(&value, &notes); err != nil {
					t.Fatal(err)
				}
				if value != 10 || notes != "Keep the integer draft" {
					t.Fatalf("correction lost draft: value=%d notes=%q", value, notes)
				}
			})
		}
	}
}

func TestIntegerInputDraftSourceAndRepresentation(t *testing.T) {
	for _, raw := range []string{"", "1e1", "NaN", "1e999", "999999999999999999999999999999999", "+12", " 12 ", `bad"><script>bad</script>`} {
		req := httptest.NewRequest(http.MethodPost, "/hosts/1?u_height=999", strings.NewReader(url.Values{"u_height": {raw}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if got := integerInputValue(req, "invalid", "u_height", 2); got.Value != raw {
			t.Errorf("raw=%q became %+v", raw, got)
		}
	}
	for _, tc := range []struct{ method, errorText, body, want string }{
		{http.MethodGet, "invalid", "", "2"},
		{http.MethodPost, "", "u_height=12", "2"},
		{http.MethodPost, "invalid", "", ""},
	} {
		req := httptest.NewRequest(tc.method, "/hosts/1?u_height=999", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if got := integerInputValue(req, tc.errorText, "u_height", 2); got.Value != tc.want {
			t.Errorf("%s/error=%q/body=%q got %+v want %q", tc.method, tc.errorText, tc.body, got, tc.want)
		}
	}
}
