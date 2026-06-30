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
	IPAM       *ipamSection
}

// ipamRow is one IP allocation as shown in the network detail IPAM table.
type ipamRow struct {
	IP        string
	HostID    int64
	HostName  string
	IsGateway bool
	Conflict  bool
}

// ipamSection is the display-ready IP occupancy of the network being viewed.
type ipamSection struct {
	TotalUsable int
	UsedCount   int
	FreeCount   int
	Unbounded   bool
	NextFree    string
	Gateway     string
	Rows        []ipamRow
}

// buildIPAMSection converts a domain.NetworkUsage into the detail-page view.
func buildIPAMSection(u domain.NetworkUsage) ipamSection {
	conflicting := map[string]bool{}
	for _, g := range u.Conflicts {
		for _, a := range g {
			conflicting[a.IP] = true
		}
	}
	rows := make([]ipamRow, 0, len(u.Used))
	for _, a := range u.Used {
		rows = append(rows, ipamRow{
			IP:        a.IP,
			HostID:    a.HostID,
			HostName:  a.HostName,
			IsGateway: a.IP == u.Network.Gateway,
			Conflict:  conflicting[a.IP],
		})
	}
	return ipamSection{
		TotalUsable: u.TotalUsable,
		UsedCount:   u.UsedCount,
		FreeCount:   u.FreeCount,
		Unbounded:   u.Unbounded,
		NextFree:    u.NextFree,
		Gateway:     u.Network.Gateway,
		Rows:        rows,
	}
}

// renderDetailExtra assembles and renders the shared detail page for one entity:
// its caller-supplied fields, its rendered Markdown notes, its tags, the
// relationships that touch it (with the other endpoint resolved to a label),
// and an optional IPAM section (nil for entities that have none).
func renderDetailExtra(
	w http.ResponseWriter,
	r *http.Request,
	cat entityCatalog,
	tags *store.TagRepo,
	rels *store.RelationshipRepo,
	entityType string, entityID int64,
	heading, notes, editURL string,
	fields []fieldRow,
	ipam *ipamSection,
) {
	tagList, err := tags.ListForEntity(entityType, entityID)
	if err != nil {
		serverError(w, r, err)
		return
	}
	edges, err := rels.ListForEntity(entityType, entityID)
	if err != nil {
		serverError(w, r, err)
		return
	}
	// Resolving relationship endpoints to labels requires loading the whole
	// entity catalog, so skip that work entirely when the entity has no edges.
	var labels map[string]string
	if len(edges) > 0 {
		opts, err := cat.options()
		if err != nil {
			serverError(w, r, err)
			return
		}
		labels = labelMap(opts)
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

	render(w, r, "detail.html", detailData{
		Title:      heading,
		Heading:    heading,
		EntityType: entityType,
		EntityID:   entityID,
		EditURL:    editURL,
		Fields:     fields,
		NotesHTML:  renderMarkdown(notes),
		Tags:       tagList,
		Related:    related,
		IPAM:       ipam,
	})
}
