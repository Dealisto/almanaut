package web

import (
	"database/sql"
	"fmt"
	"html"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

type topologyNodeView struct {
	domain.TopologyNode
	Label, URL, FocusURL string
}

type topologyCableView struct {
	From, To                         topologyNodeView
	FromPort, ToPort, FromURL, ToURL string
}

type topologyData struct {
	Title, Site, Rack, Ref, DownloadURL string
	SiteID, RackID                      int64
	Unassigned, Truncated               bool
	ChoicesTruncated                    bool
	Broken                              int
	Sites, Racks                        []domain.TopologyChoice
	Nodes                               []topologyNodeView
	Cables                              []topologyCableView
	SVG                                 template.HTML
}

func parseTopologyScope(q url.Values) (domain.TopologyScope, error) {
	var scope domain.TopologyScope
	filters := 0
	for _, key := range []string{"site", "rack"} {
		v := q.Get(key)
		if v == "" {
			continue
		}
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			return scope, fmt.Errorf("%s must be a positive record ID", key)
		}
		if key == "site" {
			scope.SiteID = id
		} else {
			scope.RackID = id
		}
		filters++
	}
	if v := q.Get("ref"); v != "" {
		typ, id, err := parseRef(v)
		if err != nil || (typ != "host" && typ != "hardware") || id <= 0 {
			return scope, fmt.Errorf("device must be host:ID or hardware:ID")
		}
		scope.Device = domain.EntityRef{Type: typ, ID: id}
		filters++
	}
	if v := q.Get("unassigned"); v != "" {
		if v != "1" {
			return scope, fmt.Errorf("unassigned must be 1")
		}
		scope.Unassigned = true
		filters++
	}
	if filters > 1 {
		return scope, fmt.Errorf("choose one scope: site, rack, device, or unassigned")
	}
	return scope, nil
}

func topologyPage(db *sql.DB, cat entityCatalog) http.HandlerFunc {
	return topologyHandler(db, cat, false)
}

func topologyDownload(db *sql.DB, cat entityCatalog) http.HandlerFunc {
	return topologyHandler(db, cat, true)
}

func topologyHandler(db *sql.DB, cat entityCatalog, download bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, err := parseTopologyScope(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		graph, err := store.ReadTopology(r.Context(), db, scope)
		if err != nil {
			notFoundOrServerError(w, r, "topology scope", err)
			return
		}
		data := topologyPresentation(graph, cat)
		data.Title = "Physical topology"
		data.Site = r.URL.Query().Get("site")
		data.Rack = r.URL.Query().Get("rack")
		data.Ref = r.URL.Query().Get("ref")
		data.Unassigned = scope.Unassigned
		data.SiteID = scope.SiteID
		data.RackID = scope.RackID
		q := url.Values{}
		if data.Site != "" {
			q.Set("site", data.Site)
		}
		if data.Rack != "" {
			q.Set("rack", data.Rack)
		}
		if data.Ref != "" {
			q.Set("ref", data.Ref)
		}
		if data.Unassigned {
			q.Set("unassigned", "1")
		}
		data.DownloadURL = "/topology.svg?" + q.Encode()
		data.SVG = topologySVG(data)
		if download {
			w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="almanaut-topology.svg"`)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_, _ = fmt.Fprint(w, data.SVG)
			return
		}
		render(w, r, "topology.html", data)
	}
}

func topologyPresentation(graph domain.Topology, cat entityCatalog) topologyData {
	data := topologyData{Truncated: graph.Truncated, Broken: graph.Broken, Sites: graph.Sites, Racks: graph.Racks, ChoicesTruncated: graph.ChoicesTruncated}
	byRef := make(map[domain.EntityRef]topologyNodeView, len(graph.Nodes))
	for _, node := range graph.Nodes {
		v := topologyNodeView{TopologyNode: node, Label: node.Ref.Type + ": " + node.Name}
		if !node.Missing {
			v.URL = cat.path(node.Ref.Type, node.Ref.ID)
			v.FocusURL = "/topology?" + url.Values{"ref": {fmt.Sprintf("%s:%d", node.Ref.Type, node.Ref.ID)}}.Encode()
		}
		byRef[node.Ref] = v
		data.Nodes = append(data.Nodes, v)
	}
	for _, c := range graph.Cables {
		data.Cables = append(data.Cables, topologyCableView{From: byRef[c.From], To: byRef[c.To], FromPort: c.FromName, ToPort: c.ToName, FromURL: cat.path("port", c.FromPort), ToURL: cat.path("port", c.ToPort)})
	}
	return data
}

// topologySVG contains escaped labels and catalog-generated local links only.
// The cable table remains the complete accessible description of the graph.
func topologySVG(data topologyData) template.HTML {
	var b strings.Builder
	height := max(240, 100+((len(data.Nodes)+3)/4)*100)
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="1000" height="%d" viewBox="0 0 1000 %d" role="img" aria-labelledby="topology-title topology-description"><title id="topology-title">Recorded physical topology</title><desc id="topology-description">%d devices and %d recorded cables. Physical connections do not establish routed reachability or outage impact.`, height, height, len(data.Nodes), len(data.Cables))
	if data.Truncated {
		b.WriteString(" View limited; narrow the scope for additional devices and cables.")
	}
	if data.Broken > 0 {
		fmt.Fprintf(&b, " %d invalid port links omitted.", data.Broken)
	}
	b.WriteString(`</desc><rect width="100%" height="100%" fill="#f8fafc"/>`)
	type point struct{ x, y int }
	positions := make([]point, len(data.Nodes))
	indices := make(map[domain.EntityRef]int, len(data.Nodes))
	for i, node := range data.Nodes {
		positions[i] = point{125 + (i%4)*250, 90 + (i/4)*100}
		indices[node.Ref] = i
	}
	// Rank cables per unordered device pair, including reversed endpoints. The
	// bounded fan uses the perpendicular of that pair, so diagonal links remain
	// distinct as well. Rank-based loops fit inside the same view box margins.
	counts, ranks := map[[2]int]int{}, map[[2]int]int{}
	pairOf := func(c topologyCableView) [2]int {
		a, z := indices[c.From.Ref], indices[c.To.Ref]
		return [2]int{min(a, z), max(a, z)}
	}
	for _, c := range data.Cables {
		counts[pairOf(c)]++
	}
	for _, c := range data.Cables {
		pair := pairOf(c)
		a, z := positions[pair[0]], positions[pair[1]]
		rank, count := ranks[pair], counts[pair]
		ranks[pair]++
		fraction := 0.5
		if count > 1 {
			fraction = float64(rank) / float64(count-1)
		}
		var path string
		if pair[0] == pair[1] {
			rx, ry := 40+45*fraction, 40+40*fraction
			path = fmt.Sprintf("M%d %d C%.3f %.3f %.3f %.3f %d %d", a.x-10, a.y, float64(a.x)-rx, float64(a.y)-ry, float64(a.x)+rx, float64(a.y)-ry, a.x+10, a.y)
		} else {
			dx, dy := float64(z.x-a.x), float64(z.y-a.y)
			length := math.Hypot(dx, dy)
			offset := (fraction - 0.5) * 128
			cx := float64(a.x+z.x)/2 - dy/length*offset
			cy := float64(a.y+z.y)/2 + dx/length*offset
			path = fmt.Sprintf("M%d %d Q%.3f %.3f %d %d", a.x, a.y, cx, cy, z.x, z.y)
		}
		fmt.Fprintf(&b, `<path d="%s" fill="none" stroke="#64748b" stroke-width="2"><title>%s / %s — %s / %s</title></path>`, path, topologyXML(c.From.Label), topologyXML(c.FromPort), topologyXML(c.To.Label), topologyXML(c.ToPort))
	}
	for i, node := range data.Nodes {
		p := positions[i]
		if node.URL != "" {
			fmt.Fprintf(&b, `<a href="%s">`, html.EscapeString(node.URL))
		}
		fill := "#dbeafe"
		if node.Outside {
			fill = "#fef3c7"
		}
		if node.Missing {
			fill = "#fee2e2"
		}
		fmt.Fprintf(&b, `<g><title>%s</title><rect x="%d" y="%d" width="218" height="42" rx="6" fill="%s" stroke="#475569"/><clipPath id="topology-label-%d"><rect x="%d" y="%d" width="198" height="36"/></clipPath><text x="%d" y="%d" clip-path="url(#topology-label-%d)" text-anchor="middle" font-family="sans-serif" font-size="12" fill="#0f172a">%s</text></g>`, topologyXML(node.Label), p.x-109, p.y-21, fill, i, p.x-99, p.y-18, p.x, p.y+4, i, topologyXML(topologyShortLabel(node.Label)))
		if node.URL != "" {
			b.WriteString("</a>")
		}
	}
	b.WriteString("</svg>")
	return template.HTML(b.String())
}

func topologyShortLabel(s string) string {
	r := []rune(s)
	if len(r) > 28 {
		return string(r[:27]) + "…"
	}
	return s
}

// XML 1.0 excludes some control characters that may exist in inventory text.
// Replace them so downloaded diagrams remain parseable as standalone SVG.
func topologyXML(s string) string {
	return html.EscapeString(strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r <= 0xd7ff) || (r >= 0xe000 && r <= 0xfffd) || (r >= 0x10000 && r <= 0x10ffff) {
			return r
		}
		return '\ufffd'
	}, s))
}
