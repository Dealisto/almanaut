package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPortGenerationErrorPreservesDraft(t *testing.T) {
	for _, tc := range []struct{ name, owner, count string }{
		{"bad count", "hardware:1", "eight"},
		{"bad owner", "service:1", "12"},
		{"malformed owner", "host:bad", "12"},
		{"missing owner", "", "12"},
		{"missing count", "hardware:1", ""},
		{"unavailable owner", "hardware:999", "eight"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			if rec := postForm(t, srv, "/hardware", url.Values{"name": {"Switch"}}); rec.Code != http.StatusSeeOther {
				t.Fatalf("create owner = %d", rec.Code)
			}
			prefix := `  Rack "A" <script> & `
			origin := "/ports?dir=desc&page=2&q=switch&sort=name"
			rec := postForm(t, srv, "/ports/generate", url.Values{"owner": {tc.owner}, "prefix": {prefix}, "count": {tc.count}, "return_to": {origin}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body)
			}
			body := rec.Body.String()
			_, generator, ok := strings.Cut(body, `action="/ports/generate"`)
			if !ok {
				t.Fatalf("generation error lost the form: %s", body)
			}
			generator, _, _ = strings.Cut(generator, "</form>")
			if got := html.UnescapeString(selectedFormOption(t, generator, "owner")); got != tc.owner {
				t.Fatalf("owner = %q, want %q", got, tc.owner)
			}
			for _, expected := range []string{`name="prefix" value="` + html.EscapeString(prefix) + `"`, `name="count" type="text" inputmode="numeric" value="` + html.EscapeString(tc.count) + `"`, `name="return_to" value="` + html.EscapeString(origin) + `"`} {
				if !strings.Contains(generator, expected) {
					t.Errorf("missing draft %q", expected)
				}
			}
			if !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `action="/ports"`) {
				t.Error("missing accessible error or canonical single-port form")
			}
			var ports, history int
			if err := db.QueryRow(`SELECT count(*) FROM ports`).Scan(&ports); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM changelog WHERE entity_type='port'`).Scan(&history); err != nil {
				t.Fatal(err)
			}
			if ports != 0 || history != 0 {
				t.Fatalf("failed generation mutated ports/history: %d/%d", ports, history)
			}
		})
	}
}

// A validation response must not commit its 400 before template execution:
// broken page data still requires a 500, without a partial HTML response.
func TestRenderStatusTemplateFailure(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/ports/generate", nil)
	rec := httptest.NewRecorder()
	renderStatus(rec, req, "port_form.html", struct{}{}, http.StatusBadRequest)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "<form") {
		t.Fatalf("template failure = %d, body=%s", rec.Code, rec.Body)
	}
}
