package web

import (
	"math"

	"github.com/Dealisto/almanaut/internal/domain"
)

// elevationUnit is one rack unit row: its U number and its visual grid row
// (row 1 = top of the rack = the highest U).
type elevationUnit struct {
	U   int
	Row int
}

// elevationOccupant is one host/hardware placed in the rack, positioned for a
// CSS grid via GridRowStart (1-based, from the top) spanning Span rows.
type elevationOccupant struct {
	Label        string
	Path         string
	Position     int // starting U (bottom of the occupant)
	UHeight      int
	GridRowStart int
	Span         int
	OutOfBounds  bool // extends past the rack top or below U1
	Overlap      bool // shares a unit with another occupant
}

// elevationSection is the display-ready rack elevation for the detail page.
type elevationSection struct {
	UHeight   int
	Units     []elevationUnit
	Occupants []elevationOccupant
}

// buildElevationSection computes the elevation for one rack from every host and
// hardware item; occupants belonging to other racks are ignored. Placement is
// best-effort: an occupant extending past the rack (OutOfBounds) is clamped for
// rendering and flagged, and any two occupants sharing a unit are flagged
// Overlap. It always returns a section (an empty rack shows just its U rail).
func buildElevationSection(rack domain.Rack, hosts []domain.Host, hardware []domain.Hardware, cat entityCatalog) *elevationSection {
	// Legacy/imported records may predate validation. Never let stored dimensions
	// determine an unbounded loop, even when integer addition would overflow.
	rackHeight := min(max(rack.UHeight, 1), domain.MaxRackUnits)
	sec := &elevationSection{UHeight: rackHeight}
	for u := rackHeight; u >= 1; u-- {
		sec.Units = append(sec.Units, elevationUnit{U: u, Row: rackHeight - u + 1})
	}

	type raw struct {
		label, path string
		pos, height int
	}
	var occs []raw
	for _, h := range hosts {
		if h.RackID == rack.ID {
			occs = append(occs, raw{h.Name, cat.path("host", h.ID), h.RackPosition, h.UHeight})
		}
	}
	for _, h := range hardware {
		if h.RackID == rack.ID {
			occs = append(occs, raw{h.Name, cat.path("hardware", h.ID), h.RackPosition, h.UHeight})
		}
	}

	// Count how many occupants claim each in-bounds unit, for overlap detection.
	claims := map[int]int{}
	for _, o := range occs {
		top := rackOccupantTop(o.pos, o.height)
		for u := max(o.pos, 1); u <= min(top, rackHeight); u++ {
			claims[u]++
		}
	}

	for _, o := range occs {
		top := rackOccupantTop(o.pos, o.height)
		oob := o.pos < 1 || top > rackHeight
		overlap := false
		for u := max(o.pos, 1); u <= min(top, rackHeight); u++ {
			if claims[u] > 1 {
				overlap = true
			}
		}
		// Clamp to the rack for rendering.
		cp := min(max(o.pos, 1), rackHeight)
		ctop := min(max(top, cp), rackHeight)
		span := ctop - cp + 1
		sec.Occupants = append(sec.Occupants, elevationOccupant{
			Label:        o.label,
			Path:         o.path,
			Position:     o.pos,
			UHeight:      o.height,
			GridRowStart: rackHeight - ctop + 1,
			Span:         span,
			OutOfBounds:  oob,
			Overlap:      overlap,
		})
	}
	return sec
}

func rackOccupantTop(position, height int) int {
	delta := max(height, 1) - 1
	if position > math.MaxInt-delta {
		return math.MaxInt
	}
	return position + delta
}
