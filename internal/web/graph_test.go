package web

import (
	"strings"
	"testing"
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
