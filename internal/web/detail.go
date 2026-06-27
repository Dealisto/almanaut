package web

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

type fieldRow struct {
	Label string
	Value string
}

type relatedItem struct {
	Text string
}

type detailData struct {
	Title      string
	Heading    string
	EntityType string
	EntityID   int64
	EditURL    string
	Fields     []fieldRow
	NotesHTML  template.HTML
	Tags       []domain.Tag
	Related    []relatedItem
}

// renderDetail assembles and renders the shared detail page for one entity:
// its caller-supplied fields, its rendered Markdown notes, its tags, and the
// relationships that touch it (with the other endpoint resolved to a label).
func renderDetail(
	w http.ResponseWriter,
	cat entityCatalog,
	tags *store.TagRepo,
	rels *store.RelationshipRepo,
	entityType string, entityID int64,
	heading, notes, editURL string,
	fields []fieldRow,
) {
	tagList, err := tags.ListForEntity(entityType, entityID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	edges, err := rels.ListForEntity(entityType, entityID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	opts, err := cat.options()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	labels := make(map[string]string, len(opts))
	for _, o := range opts {
		labels[o.Value] = o.Label
	}

	related := make([]relatedItem, 0, len(edges))
	for _, e := range edges {
		var text string
		if e.FromType == entityType && e.FromID == entityID {
			text = fmt.Sprintf("%s → %s", e.Kind, labelOrFallback(labels, e.ToType, e.ToID))
		} else {
			text = fmt.Sprintf("%s → %s", labelOrFallback(labels, e.FromType, e.FromID), e.Kind)
		}
		related = append(related, relatedItem{Text: text})
	}

	render(w, "detail.html", detailData{
		Title:      heading,
		Heading:    heading,
		EntityType: entityType,
		EntityID:   entityID,
		EditURL:    editURL,
		Fields:     fields,
		NotesHTML:  renderMarkdown(notes),
		Tags:       tagList,
		Related:    related,
	})
}
