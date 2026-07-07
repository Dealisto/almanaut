package web

import "github.com/Dealisto/almanaut/internal/domain"

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
	sec := &elevationSection{UHeight: rack.UHeight}
	for u := rack.UHeight; u >= 1; u-- {
		sec.Units = append(sec.Units, elevationUnit{U: u, Row: rack.UHeight - u + 1})
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
		height := o.height
		if height < 1 {
			height = 1
		}
		for u := o.pos; u <= o.pos+height-1; u++ {
			if u >= 1 && u <= rack.UHeight {
				claims[u]++
			}
		}
	}

	for _, o := range occs {
		height := o.height
		if height < 1 {
			height = 1
		}
		top := o.pos + height - 1
		oob := o.pos < 1 || top > rack.UHeight
		overlap := false
		for u := o.pos; u <= top; u++ {
			if u >= 1 && u <= rack.UHeight && claims[u] > 1 {
				overlap = true
			}
		}
		// Clamp to the rack for rendering.
		cp := o.pos
		if cp < 1 {
			cp = 1
		}
		ctop := top
		if ctop > rack.UHeight {
			ctop = rack.UHeight
		}
		span := ctop - cp + 1
		if span < 1 {
			span = 1
		}
		sec.Occupants = append(sec.Occupants, elevationOccupant{
			Label:        o.label,
			Path:         o.path,
			Position:     o.pos,
			UHeight:      o.height,
			GridRowStart: rack.UHeight - ctop + 1,
			Span:         span,
			OutOfBounds:  oob,
			Overlap:      overlap,
		})
	}
	return sec
}
