package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

// savedViewLink is one saved view resolved to a clickable sidebar/manage entry.
type savedViewLink struct {
	ID    int64
	Name  string
	URL   string // list route with the saved query applied
	Query string
}

// savedViewGroup groups a user's saved views under one entity type for display.
type savedViewGroup struct {
	Type      string // singular type key
	TypeTitle string // list title, e.g. "Hosts"
	BasePath  string // list route, e.g. "/hosts"
	Views     []savedViewLink
}

type savedViewsCtxKey struct{}

// currentUserID returns the authenticated user's id, or 0 when auth is disabled
// (single-user mode shares user_id 0).
func currentUserID(ctx context.Context) int64 {
	if u, ok := userFrom(ctx); ok {
		return u.ID
	}
	return 0
}

// savedViewsMiddleware loads the current user's saved views, resolves each to a
// list URL via the catalog, and stashes the grouped result in the request
// context for the sidebar. A load failure degrades to no views (logged) rather
// than failing every page.
func savedViewsMiddleware(repo *store.SavedViewRepo, cat entityCatalog) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			groups := loadSavedViewGroups(req, repo, cat)
			ctx := context.WithValue(req.Context(), savedViewsCtxKey{}, groups)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

func loadSavedViewGroups(req *http.Request, repo *store.SavedViewRepo, cat entityCatalog) []savedViewGroup {
	views, err := repo.ListForUser(currentUserID(req.Context()))
	if err != nil {
		loggerFrom(req.Context()).Printf("saved views: load failed: %v", err)
		return nil
	}
	var groups []savedViewGroup
	byType := map[string]int{} // type -> index in groups
	for _, v := range views {
		rs, ok := cat.resource(v.EntityType)
		if !ok {
			continue // an entity type that no longer exists
		}
		url := rs.basePath()
		if v.Query != "" {
			url += "?" + v.Query
		}
		link := savedViewLink{ID: v.ID, Name: v.Name, URL: url, Query: v.Query}
		if idx, seen := byType[v.EntityType]; seen {
			groups[idx].Views = append(groups[idx].Views, link)
			continue
		}
		byType[v.EntityType] = len(groups)
		groups = append(groups, savedViewGroup{
			Type: v.EntityType, TypeTitle: rs.searchHeading(), BasePath: rs.basePath(),
			Views: []savedViewLink{link},
		})
	}
	return groups
}

// savedViewsFrom returns the grouped saved views placed in the context by the
// middleware.
func savedViewsFrom(ctx context.Context) []savedViewGroup {
	g, _ := ctx.Value(savedViewsCtxKey{}).([]savedViewGroup)
	return g
}

// createSavedView stores the submitted query string under a name for the current
// user, then redirects to the list with that view applied.
func createSavedView(repo *store.SavedViewRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		entityType := strings.TrimSpace(req.FormValue("entity_type"))
		rs, ok := cat.resource(entityType)
		if !ok {
			http.Error(w, "unknown entity type", http.StatusBadRequest)
			return
		}
		view := domain.SavedView{
			UserID:     currentUserID(req.Context()),
			EntityType: entityType,
			Name:       strings.TrimSpace(req.FormValue("name")),
			Query:      strings.TrimPrefix(strings.TrimSpace(req.FormValue("query")), "?"),
			CreatedAt:  nowRFC3339(),
		}
		if err := view.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := repo.Create(view); err != nil {
			serverError(w, req, err)
			return
		}
		dest := rs.basePath()
		if view.Query != "" {
			dest += "?" + view.Query
		}
		http.Redirect(w, req, dest, http.StatusSeeOther)
	}
}

// renameSavedView renames a view the current user owns.
func renameSavedView(repo *store.SavedViewRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(req.FormValue("name"))
		if name == "" {
			http.Error(w, "view name is required", http.StatusBadRequest)
			return
		}
		if err := repo.Rename(id, currentUserID(req.Context()), name); err != nil {
			notFoundOrServerError(w, req, "view", err)
			return
		}
		http.Redirect(w, req, "/views", http.StatusSeeOther)
	}
}

// deleteSavedView deletes a view the current user owns.
func deleteSavedView(repo *store.SavedViewRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := repo.Delete(id, currentUserID(req.Context())); err != nil {
			notFoundOrServerError(w, req, "view", err)
			return
		}
		http.Redirect(w, req, "/views", http.StatusSeeOther)
	}
}

type savedViewsPageData struct {
	Title  string
	Groups []savedViewGroup
}

// savedViewsPage renders the management page listing the user's views with
// rename/delete controls.
func savedViewsPage(repo *store.SavedViewRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, req, "saved_views.html", savedViewsPageData{
			Title:  "Saved views",
			Groups: loadSavedViewGroups(req, repo, cat),
		})
	}
}
