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

// childRef is one child entity link shown in a detail page's children section.
type childRef struct {
	Label string
	Path  string
}

// childrenSection lists an entity's child entities (e.g. a Site's Locations).
type childrenSection struct {
	Title string
	Items []childRef
}

// detailExtras bundles the optional, entity-specific detail-page sections so
// renderDetailExtra keeps a fixed parameter list as more are added.
type detailExtras struct {
	ipam         *ipamSection
	children     *childrenSection
	elevation    *elevationSection
	probe        *probeSection
	customFields []domain.CustomFieldValue
	attachments  []attachmentView
}

// attachmentView is one attachment row on a detail page.
type attachmentView struct {
	ID         int64
	Filename   string
	Size       string
	UploadedAt string
}

type relatedItem struct {
	Text string
}

type detailData struct {
	Title        string
	Heading      string
	EntityType   string
	EntityID     int64
	EditURL      string
	ListURL      string
	Fields       []fieldRow
	NotesHTML    template.HTML
	Tags         []domain.Tag
	Related      []relatedItem
	GraphSVG     template.HTML
	IPAM         *ipamSection
	Children     *childrenSection
	Elevation    *elevationSection
	Probe        *probeSection
	CustomFields []domain.CustomFieldValue
	Attachments  []attachmentView

	JournalEntries []journalView
	JournalKinds   []string
	Changelog      []store.ChangeEvent
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
	TotalUsable   int
	UsedCount     int
	FreeCount     int
	Unbounded     bool
	NextFree      string
	Gateway       string
	Rows          []ipamRow
	ReservedCount int
	Reserved      []reservedRangeView
	Overlaps      []overlapView // other networks sharing this CIDR block
}

// reservedRangeView is one reservation range shown in the IPAM section.
type reservedRangeView struct {
	Name  string
	Range string
}

// overlapView is one network that occupies the same CIDR block as the one being
// viewed, shown as a conflict warning on the network detail page.
type overlapView struct {
	Name string
	CIDR string
	URL  string
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
	sec := ipamSection{
		TotalUsable: u.TotalUsable,
		UsedCount:   u.UsedCount,
		FreeCount:   u.FreeCount,
		Unbounded:   u.Unbounded,
		NextFree:    u.NextFree,
		Gateway:     u.Network.Gateway,
		Rows:        rows,
	}
	sec.ReservedCount = u.ReservedCount
	for _, r := range u.Reservations {
		sec.Reserved = append(sec.Reserved, reservedRangeView{Name: r.Name, Range: r.StartIP + "–" + r.EndIP})
	}
	return sec
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
	journal *store.JournalRepo,
	changelog *store.ChangelogRepo,
	entityType string, entityID int64,
	heading, notes, editURL, listURL string,
	fields []fieldRow,
	extras detailExtras,
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
	neighbors := make([]graphNeighbor, 0, len(edges))
	for _, e := range edges {
		if e.FromType == entityType && e.FromID == entityID {
			other := labelOrFallback(labels, e.ToType, e.ToID)
			related = append(related, relatedItem{Text: fmt.Sprintf("%s → %s", e.Kind, other)})
			neighbors = append(neighbors, graphNeighbor{Label: other, Kind: e.Kind, Outgoing: true})
		} else {
			other := labelOrFallback(labels, e.FromType, e.FromID)
			related = append(related, relatedItem{Text: fmt.Sprintf("%s → %s", other, e.Kind)})
			neighbors = append(neighbors, graphNeighbor{Label: other, Kind: e.Kind, Outgoing: false})
		}
	}

	entries, err := journal.ListForEntity(entityType, entityID)
	if err != nil {
		serverError(w, r, err)
		return
	}
	views := make([]journalView, 0, len(entries))
	for _, e := range entries {
		views = append(views, journalView{
			ID: e.ID, Kind: e.Kind, BodyHTML: renderMarkdown(e.Body), CreatedAt: e.CreatedAt,
		})
	}
	events, err := changelog.ListForEntity(entityType, entityID)
	if err != nil {
		serverError(w, r, err)
		return
	}

	render(w, r, "detail.html", detailData{
		Title:        heading,
		Heading:      heading,
		EntityType:   entityType,
		EntityID:     entityID,
		EditURL:      editURL,
		ListURL:      listURL,
		Fields:       fields,
		NotesHTML:    renderMarkdown(notes),
		Tags:         tagList,
		Related:      related,
		GraphSVG:     buildNeighborhoodSVG(heading, neighbors),
		IPAM:         extras.ipam,
		Children:     extras.children,
		Elevation:    extras.elevation,
		Probe:        extras.probe,
		CustomFields: extras.customFields,
		Attachments:  extras.attachments,

		JournalEntries: views,
		JournalKinds:   domain.JournalKinds,
		Changelog:      events,
	})
}
