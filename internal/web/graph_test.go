package web

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestBuildNeighborhoodSVGEmpty(t *testing.T) {
	if got := buildNeighborhoodSVG("Host: web", nil); got != "" {
		t.Fatalf("no neighbours should render nothing, got %q", got)
	}
}

func TestBuildNeighborhoodSVGContent(t *testing.T) {
	svg := string(buildNeighborhoodSVG("Host: web", []graphNeighbor{
		{Label: "postgres", Kind: "runs on", Outgoing: false},
		{Label: "caddy", Kind: "exposed via", Outgoing: true},
	}))
	for _, want := range []string{"<svg", "</svg>", "Host: web", "postgres", "caddy", "runs on", "exposed via", "<line"} {
		if !strings.Contains(svg, want) {
			t.Errorf("svg missing %q\n%s", want, svg)
		}
	}
	// One edge line per neighbour.
	if n := strings.Count(svg, "<line"); n != 2 {
		t.Errorf("want 2 edge lines, got %d", n)
	}
	// Outgoing vs incoming render different direction arrows.
	if !strings.Contains(svg, "→") || !strings.Contains(svg, "←") {
		t.Errorf("expected both direction arrows, got %s", svg)
	}
}

func TestBuildNeighborhoodSVGEscapesLabels(t *testing.T) {
	svg := string(buildNeighborhoodSVG("<b>c</b>", []graphNeighbor{
		{Label: "<script>alert(1)</script>", Kind: "x&y", Outgoing: true},
	}))
	if strings.Contains(svg, "<script>") {
		t.Fatalf("label not escaped: %s", svg)
	}
	if !strings.Contains(svg, "&lt;script&gt;") {
		t.Errorf("expected escaped label, got %s", svg)
	}
	if !strings.Contains(svg, "&lt;b&gt;c&lt;/b&gt;") {
		t.Errorf("expected escaped center, got %s", svg)
	}
	if !strings.Contains(svg, "x&amp;y") {
		t.Errorf("expected escaped kind, got %s", svg)
	}
}

func TestDetailPageShowsGraphWhenRelated(t *testing.T) {
	h, db := newTestServerDockerDB(t, fakeScanner{})
	hosts := store.NewHostRepo(db)
	svcs := store.NewServiceRepo(db)
	rels := store.NewRelationshipRepo(db)
	hid, err := hosts.Create(domain.Host{Name: "web", Type: "vm"})
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	sid, err := svcs.Create(domain.Service{Name: "grafana", Kind: "container"})
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	if _, err := rels.Create(domain.Relationship{
		FromType: "service", FromID: sid, ToType: "host", ToID: hid, Kind: "runs on",
	}); err != nil {
		t.Fatalf("rel: %v", err)
	}

	// Host detail has one relationship -> graph present, neighbour labelled.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hosts/"+itoaGraph(hid), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<svg") || !strings.Contains(body, `class="depgraph"`) {
		t.Errorf("expected graph svg on related detail page")
	}
	if !strings.Contains(body, "grafana") {
		t.Errorf("expected neighbour label in graph")
	}

	// An unrelated entity should not render a graph.
	uid, err := hosts.Create(domain.Host{Name: "lonely", Type: "vm"})
	if err != nil {
		t.Fatalf("host2: %v", err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hosts/"+itoaGraph(uid), nil))
	if strings.Contains(rec.Body.String(), "<svg") {
		t.Errorf("entity with no relationships should not render a graph")
	}
}

func itoaGraph(n int64) string { return strconv.FormatInt(n, 10) }
