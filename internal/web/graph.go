package web

import (
	"fmt"
	"html"
	"html/template"
	"math"
	"strings"
)

// graphNeighbor is one entity directly related to the entity at the graph's
// centre. Outgoing is true when the centre is the relationship's "from" end.
type graphNeighbor struct {
	Label    string
	Kind     string
	Outgoing bool
}

// buildNeighborhoodSVG renders the centre entity and its direct neighbours as a
// radial SVG: the centre in the middle, neighbours evenly spaced on a circle,
// each edge a line labelled with its relationship kind and a direction arrow.
// Returns "" when there are no neighbours. All user text is HTML-escaped;
// colours use currentColor so the graph inherits the page theme.
func buildNeighborhoodSVG(center string, neighbors []graphNeighbor) template.HTML {
	if len(neighbors) == 0 {
		return ""
	}
	const (
		w, h   = 640, 460
		cx, cy = w / 2, h / 2
		radius = 170.0
	)
	n := len(neighbors)
	xs := make([]float64, n)
	ys := make([]float64, n)
	for i := range neighbors {
		ang := -math.Pi/2 + 2*math.Pi*float64(i)/float64(n)
		xs[i] = float64(cx) + radius*math.Cos(ang)
		ys[i] = float64(cy) + radius*math.Sin(ang)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" class="depgraph" role="img" xmlns="http://www.w3.org/2000/svg">`, w, h)

	// Edges first, so nodes and labels draw on top of the lines.
	for i, nb := range neighbors {
		fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%.1f" y2="%.1f" stroke="currentColor" stroke-opacity="0.35" stroke-width="1.5"/>`,
			cx, cy, xs[i], ys[i])
		mx, my := (float64(cx)+xs[i])/2, (float64(cy)+ys[i])/2
		arrow := "→"
		if !nb.Outgoing {
			arrow = "←"
		}
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" text-anchor="middle" font-size="10" fill="currentColor" fill-opacity="0.7">%s %s</text>`,
			mx, my, html.EscapeString(nb.Kind), arrow)
	}

	// Neighbour nodes + labels.
	for i, nb := range neighbors {
		fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="6" fill="currentColor" fill-opacity="0.55"/>`, xs[i], ys[i])
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" text-anchor="middle" font-size="12" fill="currentColor">%s</text>`,
			xs[i], ys[i]-12, html.EscapeString(nb.Label))
	}

	// Centre node + label.
	fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="9" fill="currentColor"/>`, cx, cy)
	fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" font-size="13" font-weight="bold" fill="currentColor">%s</text>`,
		cx, cy-15, html.EscapeString(center))

	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}
