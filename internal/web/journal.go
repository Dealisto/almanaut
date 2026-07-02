package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/go-chi/chi/v5"
)

// journalView is one journal entry ready for the detail page: its Markdown
// body is pre-rendered to HTML the same way notes are.
type journalView struct {
	ID        int64
	Kind      string
	BodyHTML  template.HTML
	CreatedAt string
}

// addJournal appends a journal entry to this resource's entity.
func (rs resource[T]) addJournal(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, ok := rs.idParam(w, req)
		if !ok {
			return
		}
		entry := domain.JournalEntry{
			EntityType: rs.sing, EntityID: id,
			Kind:      req.FormValue("kind"),
			Body:      strings.TrimSpace(req.FormValue("body")),
			CreatedAt: nowRFC3339(),
		}
		if err := entry.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := d.journal.Create(entry); err != nil {
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, fmt.Sprintf("%s/%d", rs.basePath(), id), http.StatusSeeOther)
	}
}

// deleteJournal removes one entry and redirects back to its entity's detail page.
func deleteJournal(cat entityCatalog, d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		entry, err := d.journal.Get(id)
		if err != nil {
			notFoundOrServerError(w, req, "journal entry", err)
			return
		}
		if err := d.journal.Delete(id); err != nil {
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, cat.path(entry.EntityType, entry.EntityID), http.StatusSeeOther)
	}
}
