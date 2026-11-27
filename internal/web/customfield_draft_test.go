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

func customFieldCreateForm(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`(?s)<section class="panel" id="add-field">.*?</form>`).FindString(body)
	if match == "" {
		t.Fatal("missing custom-field create form")
	}
	return match
}

func TestCustomFieldCreateErrorsPreserveSubmittedDraft(t *testing.T) {
	for _, failure := range []string{"duplicate", "invalid name", "invalid type", "invalid kind", "missing type", "missing kind"} {
		t.Run(failure, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			original := url.Values{"label": {"Existing amount"}, "name": {"monthly_cost"}, "entity_type": {"service"}, "kind": {"number"}}
			if rec := postForm(t, srv, "/custom-fields", original); rec.Code != http.StatusSeeOther {
				t.Fatalf("create original: %d", rec.Code)
			}
			label := `Monthly "amount" & <script>window.injected = true</script>`
			form := url.Values{"label": {label}, "name": {"Monthly Cost"}, "entity_type": {"service"}, "kind": {"number"}}
			switch failure {
			case "invalid name":
				form.Set("name", "!!!")
			case "invalid type":
				form.Set("entity_type", "removed-type")
			case "invalid kind":
				form.Set("kind", "removed-kind")
			case "missing type":
				form.Del("entity_type")
			case "missing kind":
				form.Del("kind")
			}
			rec := postForm(t, srv, "/custom-fields", form)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `class="error"`) {
				t.Fatalf("expected form validation: %d %s", rec.Code, rec.Body)
			}
			body := customFieldCreateForm(t, rec.Body.String())
			for _, name := range []string{"label", "name"} {
				match := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(body)
				if len(match) != 2 || html.UnescapeString(match[1]) != form.Get(name) {
					t.Errorf("lost submitted %s: %v", name, match)
				}
			}
			for _, name := range []string{"entity_type", "kind"} {
				selectHTML := regexp.MustCompile(`(?s)<select name="` + name + `".*?</select>`).FindString(body)
				if !strings.Contains(selectHTML, `value="`+form.Get(name)+`" selected`) {
					t.Errorf("lost selected %s=%q: %s", name, form.Get(name), selectHTML)
				}
			}
			if !strings.Contains(body, `<details class="disclosure" open>`) || strings.Contains(body, "<script>") {
				t.Error("advanced draft must be visible and safely escaped")
			}
			defs, err := store.NewCustomFieldRepo(db).ListAllDefs()
			if err != nil || len(defs) != 1 || defs[0].Label != "Existing amount" {
				t.Fatalf("failed submission changed definitions: %+v %v", defs, err)
			}
		})
	}
}

func TestCustomFieldLabelErrorKeepsOnlyFailedEditorOpen(t *testing.T) {
	srv, _ := newTestServerDB(t)
	for _, name := range []string{"first", "second"} {
		if rec := postForm(t, srv, "/custom-fields", url.Values{"label": {name}, "entity_type": {"service"}, "kind": {"number"}}); rec.Code != http.StatusSeeOther {
			t.Fatalf("create %s: %d", name, rec.Code)
		}
	}
	rec := postForm(t, srv, "/custom-fields/2", url.Values{"label": {"   "}})
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !regexp.MustCompile(`(?s)<details class="disclosure" open><summary>Edit field.*?<form[^>]+action="/custom-fields/2"[^>]*>.*?name="label" value="   "`).MatchString(body) {
		t.Fatalf("failed label editor lost draft/open state: status=%d", rec.Code)
	}
	if count := strings.Count(body, `<details class="disclosure" open>`); count != 1 {
		t.Errorf("opened %d editors, want only failed editor", count)
	}
	if !strings.Contains(customFieldCreateForm(t, body), `name="label" value=""`) {
		t.Error("label edit error incorrectly populated the create form")
	}
}
