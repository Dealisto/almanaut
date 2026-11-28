package web

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestTopologyPageAndDownload(t *testing.T) {
	srv, db := newTestServerDB(t)
	for _, statement := range []string{
		`INSERT INTO hosts(id,name,type) VALUES(1,'server <script>','physical')`,
		`INSERT INTO hardware(id,name) VALUES(1,'switch & router')`,
		`INSERT INTO ports(id,owner_type,owner_id,name,peer_port_id) VALUES(1,'host',1,'eth0',2),(2,'hardware',1,'uplink',1)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/topology?ref=host:1", nil))
	if rec.Code != 200 {
		t.Fatalf("page: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{"Physical topology", "server &lt;script&gt;", "switch &amp; router", `href="/hardware/1"`, `href="/ports/2"`, "Outside selection", "Download SVG", "1 cables", "Connections"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, "server <script>") || strings.Contains(body, "/hardwares/") {
		t.Fatalf("unsafe or incorrect links: %s", body)
	}
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/topology.svg?ref=host:1", nil))
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "image/svg+xml") || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: %d %v", rec.Code, rec.Header())
	}
	d := xml.NewDecoder(strings.NewReader(rec.Body.String()))
	for {
		_, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid SVG: %v", err)
		}
	}
	if strings.Contains(rec.Body.String(), "<script>") {
		t.Fatal("unescaped SVG label")
	}
}

func TestTopologySelfCableAndMissingRecord(t *testing.T) {
	ref := domain.EntityRef{Type: "host", ID: 1}
	missing := domain.EntityRef{Type: "host", ID: 99}
	data := topologyPresentation(domain.Topology{Nodes: []domain.TopologyNode{{Ref: ref, Name: "local"}, {Ref: missing, Name: "Missing host #99", Missing: true, Outside: true}}, Cables: []domain.TopologyCable{{From: ref, To: ref, FromPort: 1, ToPort: 2, FromName: "loop-a", ToName: "loop-b"}, {From: ref, To: missing, FromPort: 3, ToPort: 4}}}, entityCatalog{})
	if data.Nodes[1].URL != "" || data.Nodes[1].FocusURL != "" {
		t.Fatal("missing record is navigable")
	}
	svg := string(topologySVG(data))
	if strings.Count(svg, "<path ") != 2 || !strings.Contains(svg, " C") || strings.Contains(svg, `href="/hosts/99"`) {
		t.Fatalf("self or missing topology: %s", svg)
	}
	data.Nodes[0].Label = "control\x01 <&> device"
	d := xml.NewDecoder(strings.NewReader(string(topologySVG(data))))
	for {
		_, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("inventory control character broke SVG: %v", err)
		}
	}
}

func TestTopologyRejectsBadScopesAndMissingDevice(t *testing.T) {
	srv, _ := newTestServerDB(t)
	for _, path := range []string{"/topology?site=-1", "/topology?rack=abc", "/topology?ref=service:1", "/topology?ref=host:0", "/topology?site=1&rack=1", "/topology.svg?unassigned=yes"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 400 {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/topology?ref=host:999", nil))
	if rec.Code != 404 {
		t.Fatalf("missing device: %d", rec.Code)
	}
	body := getBody(t, srv, "/topology")
	if !strings.Contains(body, "No devices match this selection.") {
		t.Fatal("missing empty state")
	}
}

func TestTopologyParallelDiagonalCablesUseDistinctPerpendicularPaths(t *testing.T) {
	graph := domain.Topology{}
	for i := range 21 {
		graph.Nodes = append(graph.Nodes, domain.TopologyNode{Ref: domain.EntityRef{Type: "host", ID: int64(i + 1)}, Name: fmt.Sprintf("node %d", i)})
	}
	// These grid positions have dx=-dy: an offset of (bend,-bend) would
	// merely move controls along the line and hide every parallel cable.
	for i := range 200 {
		from, to := graph.Nodes[2].Ref, graph.Nodes[20].Ref
		if i%2 != 0 {
			from, to = to, from
		}
		graph.Cables = append(graph.Cables, domain.TopologyCable{From: from, To: to, FromPort: int64(2*i + 1), ToPort: int64(2*i + 2)})
	}
	paths := topologyTestPaths(t, string(topologySVG(topologyPresentation(graph, entityCatalog{}))))
	if len(paths) != 200 {
		t.Fatalf("rendered %d cables, want 200", len(paths))
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			t.Fatalf("parallel cable repeats another curve: %s", path)
		}
		seen[path] = true
		var ax, ay, cx, cy, zx, zy float64
		if n, err := fmt.Sscanf(path, "M%f %f Q%f %f %f %f", &ax, &ay, &cx, &cy, &zx, &zy); err != nil || n != 6 {
			t.Fatalf("invalid quadratic path %q: %v", path, err)
		}
		dx, dy := zx-ax, zy-ay
		ox, oy := cx-(ax+zx)/2, cy-(ay+zy)/2
		if math.Abs(ox*dx+oy*dy) > 1 || math.Abs(ox*dy-oy*dx) < 1 {
			t.Fatalf("control offset is not perpendicular and nonzero: %s", path)
		}
		if cx < 0 || cx > 1000 || cy < 0 || cy > 700 {
			t.Fatalf("curve control outside SVG: %s", path)
		}
	}
}

func TestTopologyParallelSelfLoopsHaveDistinctBoundedRadii(t *testing.T) {
	ref := domain.EntityRef{Type: "host", ID: 1}
	graph := domain.Topology{Nodes: []domain.TopologyNode{{Ref: ref, Name: "loop device"}}}
	for i := range 200 {
		graph.Cables = append(graph.Cables, domain.TopologyCable{From: ref, To: ref, FromPort: int64(2*i + 1), ToPort: int64(2*i + 2)})
	}
	paths := topologyTestPaths(t, string(topologySVG(topologyPresentation(graph, entityCatalog{}))))
	if len(paths) != 200 {
		t.Fatalf("rendered %d loops, want 200", len(paths))
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			t.Fatalf("self loop repeats another curve: %s", path)
		}
		seen[path] = true
		var ax, ay, cx, cy, dx, dy, zx, zy float64
		if n, err := fmt.Sscanf(path, "M%f %f C%f %f %f %f %f %f", &ax, &ay, &cx, &cy, &dx, &dy, &zx, &zy); err != nil || n != 8 {
			t.Fatalf("invalid loop %q: %v", path, err)
		}
		for _, p := range [][2]float64{{ax, ay}, {cx, cy}, {dx, dy}, {zx, zy}} {
			if p[0] < 0 || p[0] > 1000 || p[1] < 0 || p[1] > 240 {
				t.Fatalf("loop outside SVG: %s", path)
			}
		}
		if cy >= ay || dy >= zy {
			t.Fatalf("loop collapsed into its device: %s", path)
		}
	}
}

func TestTopologyWideLabelsAreClippedWithFullAccessibleTitle(t *testing.T) {
	label := strings.Repeat("網", 27) + "🛰"
	data := topologyData{Nodes: []topologyNodeView{{TopologyNode: domain.TopologyNode{Ref: domain.EntityRef{Type: "host", ID: 1}}, Label: label}}}
	type rect struct {
		X     float64 `xml:"x,attr"`
		Width float64 `xml:"width,attr"`
	}
	var svg struct {
		Groups []struct {
			Title string `xml:"title"`
			Rect  rect   `xml:"rect"`
			Clip  struct {
				ID   string `xml:"id,attr"`
				Rect rect   `xml:"rect"`
			} `xml:"clipPath"`
			Text struct {
				Clip  string `xml:"clip-path,attr"`
				Label string `xml:",chardata"`
			} `xml:"text"`
		} `xml:"g"`
	}
	if err := xml.Unmarshal([]byte(topologySVG(data)), &svg); err != nil {
		t.Fatal(err)
	}
	if len(svg.Groups) != 1 {
		t.Fatalf("missing device group: %+v", svg)
	}
	g := svg.Groups[0]
	if g.Title != label || g.Text.Label != label || data.Nodes[0].Label != label {
		t.Fatal("wide label lost its accessible full title or table label")
	}
	if g.Clip.ID == "" || g.Text.Clip != "url(#"+g.Clip.ID+")" || g.Clip.Rect.Width <= 0 || g.Clip.Rect.X <= g.Rect.X || g.Clip.Rect.X+g.Clip.Rect.Width >= g.Rect.X+g.Rect.Width {
		t.Fatalf("wide text is not clipped inside the node bounds: %+v", g)
	}
}

func topologyTestPaths(t *testing.T, svg string) []string {
	t.Helper()
	var doc struct {
		Paths []struct {
			D string `xml:"d,attr"`
		} `xml:"path"`
	}
	if err := xml.Unmarshal([]byte(svg), &doc); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(doc.Paths))
	for _, path := range doc.Paths {
		paths = append(paths, path.D)
	}
	return paths
}
