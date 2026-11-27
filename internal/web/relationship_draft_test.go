package web

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
)

func assertRelationshipDraft(t *testing.T, body string, form url.Values) {
	t.Helper()
	for _, name := range []string{"from", "kind", "to"} {
		selectHTML := regexp.MustCompile(`(?s)<select name="` + name + `"[^>]*>.*?</select>`).FindString(body)
		selected := regexp.MustCompile(`<option value="([^"]*)" selected>`).FindStringSubmatch(selectHTML)
		if len(selected) != 2 || html.UnescapeString(selected[1]) != form.Get(name) {
			t.Errorf("lost selected %s=%q: %s", name, form.Get(name), selectHTML)
		}
	}
}

func TestRelationshipErrorsPreserveSubmittedSelections(t *testing.T) {
	for _, tc := range []struct{ name, from, kind, to string }{
		{"self-link", "service:1", "depends on", "service:1"},
		{"invalid kind", "service:1", `unsupported "kind"`, "host:2"},
		{"invalid source", `bad\"><script>window.injected=true</script>`, "owned by", "host:2"},
		{"invalid destination", "service:1", "backed up by", "not-an-entity"},
		{"deleted source", "service:99", "secured by", "host:2"},
		{"deleted destination", "service:1", "connected to", "host:99"},
		{"missing source", "", "exposed via", "host:2"},
		{"missing kind", "service:1", "", "host:2"},
		{"missing destination", "service:1", "administered by", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			for _, name := range []string{"first host", "second host"} {
				if rec := postForm(t, srv, "/hosts", url.Values{"name": {name}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
					t.Fatalf("create host: %d", rec.Code)
				}
			}
			if rec := postForm(t, srv, "/services", url.Values{"name": {"service"}, "kind": {"native"}}); rec.Code != http.StatusSeeOther {
				t.Fatalf("create service: %d", rec.Code)
			}
			form := url.Values{"from": {tc.from}, "kind": {tc.kind}, "to": {tc.to}}
			rec := postForm(t, srv, "/relationships", form)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `role="alert"`) {
				t.Fatalf("expected form error: %d %s", rec.Code, rec.Body)
			}
			assertRelationshipDraft(t, rec.Body.String(), form)
			if strings.Contains(rec.Body.String(), "<script>") {
				t.Error("submitted reference became executable markup")
			}
			if rows, err := store.NewRelationshipRepo(db).List(); err != nil || len(rows) != 0 {
				t.Fatalf("invalid relationship changed inventory: %+v %v", rows, err)
			}
		})
	}
}

func TestRelationshipErrorKeepsDraftWhenEveryEntityDisappeared(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"from": {"service:7"}, "kind": {"depends on"}, "to": {"host:9"}}
	rec := postForm(t, srv, "/relationships", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("missing endpoints status=%d", rec.Code)
	}
	assertRelationshipDraft(t, rec.Body.String(), form)
	if !strings.Contains(rec.Body.String(), `type="submit" disabled>Add relationship`) {
		t.Error("no available entities should leave the draft visible but disable submission")
	}
	if !strings.Contains(rec.Body.String(), "Create some entities first") {
		t.Error("missing recovery guidance when no entities remain")
	}
}
