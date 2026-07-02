package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// actor returns the Basic-auth username making the request, or "" when the app
// is unauthenticated. The M2 API-token work will extend this.
func actor(req *http.Request) string {
	user, _, _ := req.BasicAuth()
	return user
}

// nowRFC3339 is the single timestamp format used across history rows.
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

type historyRow struct {
	Label      string
	EntityType string
	Path       string // "" when the entity no longer exists (or is a delete/import row)
	Action     string
	Actor      string
	Changes    []domain.FieldChange
	CreatedAt  string
}

type historyData struct {
	Title string
	Rows  []historyRow
}

// buildActivityRows turns changelog events into display rows, linking each to
// its entity's detail page when that entity still exists. Delete/import rows
// (and rows whose entity is gone) show the frozen label, unlinked.
func buildActivityRows(cat entityCatalog, events []store.ChangeEvent) []historyRow {
	opts, err := cat.options()
	live := map[string]bool{}
	if err == nil {
		for _, o := range opts {
			live[o.Value] = true
		}
	}
	rows := make([]historyRow, 0, len(events))
	for _, e := range events {
		path := ""
		key := fmt.Sprintf("%s:%d", e.EntityType, e.EntityID)
		if e.Action != domain.ActionDelete && e.Action != domain.ActionImport && live[key] {
			path = cat.path(e.EntityType, e.EntityID)
		}
		rows = append(rows, historyRow{
			Label: e.Label, EntityType: e.EntityType, Path: path,
			Action: e.Action, Actor: e.Actor, Changes: e.Changes, CreatedAt: e.CreatedAt,
		})
	}
	return rows
}

// history renders the global activity feed (most recent first).
func history(cat entityCatalog, changelog *store.ChangelogRepo) http.HandlerFunc {
	const max = 200
	return func(w http.ResponseWriter, req *http.Request) {
		events, err := changelog.ListRecent(max)
		if err != nil {
			serverError(w, req, err)
			return
		}
		render(w, req, "history.html", historyData{Title: "History", Rows: buildActivityRows(cat, events)})
	}
}
