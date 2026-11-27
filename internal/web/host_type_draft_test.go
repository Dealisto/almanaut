package web

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
)

func TestHostFormPreservesUnavailableType(t *testing.T) {
	for _, edit := range []bool{false, true} {
		for _, submitted := range []string{"retired", "unknown", "", `bad"><script>bad</script>`} {
			t.Run(fmt.Sprintf("edit=%t/type=%s", edit, submitted), func(t *testing.T) {
				srv, db := newTestServerDB(t)
				post := func(path string, form url.Values, status int) string {
					t.Helper()
					rec := postForm(t, srv, path, form)
					if rec.Code != status {
						t.Fatalf("POST %s = %d: %s", path, rec.Code, rec.Body)
					}
					return rec.Body.String()
				}
				post("/host-types", url.Values{"name": {"retired"}, "label": {"Retired type"}}, http.StatusSeeOther)
				path := "/hosts"
				if edit {
					post(path, url.Values{"name": {"Original"}, "type": {"retired"}}, http.StatusSeeOther)
					path += "/1"
					if got := selectedFormOption(t, getPage(t, srv, path+"/edit"), "type"); got != "retired" {
						t.Fatalf("initial selected type = %q", got)
					}
				}
				post("/host-types/retired/delete", url.Values{"replacement": {"vm"}}, http.StatusSeeOther)
				var history int
				if err := db.QueryRow(`SELECT count(*) FROM changelog`).Scan(&history); err != nil {
					t.Fatal(err)
				}
				form := url.Values{"name": {"Pending"}, "type": {submitted}, "notes": {"Keep this draft"}}
				for attempt := 0; attempt < 2; attempt++ {
					body := post(path, form, http.StatusOK)
					if !strings.Contains(body, `role="alert"`) {
						t.Fatal("missing validation error")
					}
					selected := html.UnescapeString(selectedFormOption(t, body, "type"))
					if selected != submitted {
						t.Fatalf("type became %q instead of %q", selected, submitted)
					}
					if strings.Contains(body, "<script>bad</script>") {
						t.Fatal("unescaped type")
					}
					form.Set("type", selected)
					hosts, err := store.NewHostRepo(db).List()
					if err != nil {
						t.Fatal(err)
					}
					if edit {
						if len(hosts) != 1 || hosts[0].Type != "vm" || hosts[0].Name != "Original" {
							t.Fatalf("retry overwrote reassigned host: %+v", hosts)
						}
					} else if len(hosts) != 0 {
						t.Fatalf("invalid type created a host: %+v", hosts)
					}
					var actual int
					if err := db.QueryRow(`SELECT count(*) FROM changelog`).Scan(&actual); err != nil {
						t.Fatal(err)
					}
					if actual != history {
						t.Fatalf("invalid draft added history: %d -> %d", history, actual)
					}
				}
				form.Set("type", "vm")
				post(path, form, http.StatusSeeOther)
				host, err := store.NewHostRepo(db).Get(1)
				if err != nil || host.Type != "vm" || host.Name != "Pending" || host.Notes != "Keep this draft" {
					t.Fatalf("explicit correction lost draft: %+v %v", host, err)
				}
			})
		}
	}
}
