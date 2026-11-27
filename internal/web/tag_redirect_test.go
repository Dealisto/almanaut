package web

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestRemoveTagRedirectUsesRegisteredEntityPaths(t *testing.T) {
	for _, tc := range []struct{ name, typ, id, want string }{
		{"protocol relative", "/attacker.example/", "1", "/tags"},
		{"backslash authority", `\attacker.example\`, "1", "/tags"},
		{"unknown type", "invented", "1", "/tags"},
		{"path traversal", "../account", "1", "/tags"},
		{"missing id", "hardware", "", "/tags"},
		{"negative id", "hardware", "-1", "/tags"},
		{"zero id", "hardware", "0", "/tags"},
		{"canonical irregular route", "hardware", "1", "/hardware/1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			if _, err := store.NewHardwareRepo(db).Create(domain.Hardware{Name: "switch"}); err != nil {
				t.Fatal(err)
			}
			tags := store.NewTagRepo(db)
			if err := tags.Add(domain.Tag{EntityType: "hardware", EntityID: 1, Name: "prod"}); err != nil {
				t.Fatal(err)
			}
			rec := postForm(t, srv, "/tags/delete", url.Values{"id": {"1"}, "entity_type": {tc.typ}, "entity_id": {tc.id}})
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != tc.want {
				t.Errorf("redirect = %d %q, want 303 %q", rec.Code, rec.Header().Get("Location"), tc.want)
			}
			remaining, err := tags.ListForEntity("hardware", 1)
			if err != nil || len(remaining) != 0 {
				t.Fatalf("invalid return metadata prevented authorized removal: %+v, %v", remaining, err)
			}
		})
	}
}
