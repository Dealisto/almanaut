package web

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestTagCloudLinksPreserveLeadingHashIdentity(t *testing.T) {
	srv, db := newTestServerDB(t)
	tags := store.NewTagRepo(db)
	for i, name := range []string{"prod", "#prod", "##prod", "#"} {
		id, err := store.NewHostRepo(db).Create(domain.Host{Name: "tagged-host-" + itoa(int64(i+1)), Type: "vm"})
		if err != nil {
			t.Fatal(err)
		}
		if err := tags.Add(domain.Tag{EntityType: "host", EntityID: id, Name: "#" + name}); err != nil {
			t.Fatal(err)
		}
	}
	body := getBody(t, srv, "/tags")
	links := regexp.MustCompile(`class="tag" href="([^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(links) != 4 {
		t.Fatalf("cloud links: %+v", links)
	}
	seen := map[string]bool{}
	for _, link := range links {
		path := html.UnescapeString(link[1])
		u, err := url.Parse(path)
		if err != nil || u.Path != "/tags" || u.IsAbs() {
			t.Fatalf("invalid tag URL: %q %v", path, err)
		}
		name := domain.NormalizeTag(u.Query().Get("name"))
		if seen[name] {
			t.Fatalf("distinct tags collapsed into the same query: %q", name)
		}
		seen[name] = true
		var want string
		switch name {
		case "prod":
			want = "tagged-host-1"
		case "#prod":
			want = "tagged-host-2"
		case "##prod":
			want = "tagged-host-3"
		case "#":
			want = "tagged-host-4"
		default:
			t.Fatalf("tag link discarded its identity: %q", path)
		}
		page := getBody(t, srv, path)
		if strings.Count(page, "tagged-host-") != 1 || !strings.Contains(page, want) {
			t.Fatalf("tag link %q selected wrong entities; expected %s", path, want)
		}
	}
	// Direct raw input must normalize only once, independently of cloud links.
	rec := getJSONResp(t, srv, "/tags?name="+url.QueryEscape("##prod"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "tagged-host-2") || strings.Contains(rec.Body.String(), "tagged-host-1") {
		t.Fatal("direct tag query normalized a canonical name a second time")
	}
}
