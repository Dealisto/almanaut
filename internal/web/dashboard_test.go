package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestGroupServices(t *testing.T) {
	svcs := []domain.Service{
		{ID: 1, Name: "Jellyfin", URL: "http://nas:8096", Category: "Media"},
		{ID: 2, Name: "PostgreSQL", Category: "Database"}, // no URL
		{ID: 3, Name: "Nginx Proxy Manager", URL: "http://nas:81", Category: "Media"},
		{ID: 4, Name: "orphan", Category: ""},                         // blank category
		{ID: 5, Name: "spaces-url", URL: "   ", Category: "Database"}, // whitespace-only URL → no url
		{ID: 6, Name: "spaces-cat", Category: "  "},                   // whitespace-only category → Uncategorized
	}
	groups := groupServices(svcs)
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	// Sorted case-insensitively by category: Database, Media, Uncategorized.
	if groups[0].Category != "Database" || groups[1].Category != "Media" || groups[2].Category != "Uncategorized" {
		t.Fatalf("order = %q/%q/%q", groups[0].Category, groups[1].Category, groups[2].Category)
	}
	// Media sorted by name: Jellyfin, Nginx Proxy Manager.
	media := groups[1].Services
	if len(media) != 2 || media[0].Name != "Jellyfin" || media[1].Name != "Nginx Proxy Manager" {
		t.Fatalf("media = %+v", media)
	}
	if !media[0].External || media[0].Href != "http://nas:8096" {
		t.Errorf("Jellyfin = %+v, want external to its URL", media[0])
	}
	// No URL → internal detail link, not external.
	pg := groups[0].Services[0]
	if pg.External || pg.Href != "/services/2" {
		t.Errorf("PostgreSQL = %+v, want internal /services/2", pg)
	}
	// Whitespace-only URL is treated as no URL → internal detail link, not external.
	// Database sorted by name: PostgreSQL, spaces-url.
	db := groups[0].Services
	if len(db) != 2 || db[1].Name != "spaces-url" {
		t.Fatalf("database = %+v", db)
	}
	if db[1].External || db[1].Href != "/services/5" {
		t.Errorf("spaces-url = %+v, want internal /services/5", db[1])
	}
	// Whitespace-only category collapses into Uncategorized, alongside the blank one.
	unc := groups[2].Services
	if len(unc) != 2 || unc[0].Name != "orphan" || unc[1].Name != "spaces-cat" {
		t.Fatalf("uncategorized = %+v", unc)
	}
}

func TestAnyAttention(t *testing.T) {
	if anyAttention([]attentionGroup{{Title: "a"}, {Title: "b"}}) {
		t.Error("no items → want false")
	}
	if !anyAttention([]attentionGroup{{Title: "a", Items: []attentionItem{{Label: "x"}}}}) {
		t.Error("has items → want true")
	}
}

// TestDashboardShowsRecentActivity drives a create through the real handler
// and asserts the dashboard's "Recent activity" panel renders it.
func TestDashboardShowsRecentActivity(t *testing.T) {
	srv, _ := newTestServerDB(t)

	rec := postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}, "status": {"running"}})
	if rec.Code != 303 {
		t.Fatalf("POST /hosts = %d, want 303", rec.Code)
	}

	body := getBody(t, srv, "/")
	if !strings.Contains(body, "Recent activity") || !strings.Contains(body, "nas") {
		t.Errorf("dashboard missing recent activity:\n%s", body)
	}
}
