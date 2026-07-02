package web

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestGroupServices(t *testing.T) {
	svcs := []domain.Service{
		{ID: 1, Name: "Jellyfin", URL: "http://nas:8096", Category: "Media"},
		{ID: 2, Name: "PostgreSQL", Category: "Database"}, // no URL
		{ID: 3, Name: "Nginx Proxy Manager", URL: "http://nas:81", Category: "Media"},
		{ID: 4, Name: "orphan", Category: ""}, // blank category
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
}

func TestAnyAttention(t *testing.T) {
	if anyAttention([]attentionGroup{{Title: "a"}, {Title: "b"}}) {
		t.Error("no items → want false")
	}
	if !anyAttention([]attentionGroup{{Title: "a", Items: []attentionItem{{Label: "x"}}}}) {
		t.Error("has items → want true")
	}
}
