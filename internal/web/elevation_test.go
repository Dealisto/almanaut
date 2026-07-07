package web

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestBuildElevationPlacesOccupant(t *testing.T) {
	sec := buildElevationSection(domainRack(5, 10), []domainHostT{{ID: 1, Name: "a", RackID: 5, RackPosition: 3, UHeight: 2}}, nil, testCatalog())
	if sec.UHeight != 10 {
		t.Fatalf("UHeight = %d, want 10", sec.UHeight)
	}
	if len(sec.Units) != 10 {
		t.Fatalf("Units = %d, want 10", len(sec.Units))
	}
	if len(sec.Occupants) != 1 {
		t.Fatalf("Occupants = %d, want 1", len(sec.Occupants))
	}
	o := sec.Occupants[0]
	// Occupant at U3, 2U tall occupies U3..U4; top unit U4; visual row = 10-4+1 = 7; span 2.
	if o.GridRowStart != 7 || o.Span != 2 {
		t.Fatalf("placement GridRowStart=%d Span=%d, want 7/2", o.GridRowStart, o.Span)
	}
	if o.OutOfBounds || o.Overlap {
		t.Fatalf("unexpected flags: oob=%v overlap=%v", o.OutOfBounds, o.Overlap)
	}
}

func TestBuildElevationFlagsOverlap(t *testing.T) {
	sec := buildElevationSection(domainRack(5, 10), []domainHostT{
		{ID: 1, Name: "a", RackID: 5, RackPosition: 1, UHeight: 3},
		{ID: 2, Name: "b", RackID: 5, RackPosition: 2, UHeight: 1},
	}, nil, testCatalog())
	for _, o := range sec.Occupants {
		if !o.Overlap {
			t.Fatalf("expected overlap flag on %s", o.Label)
		}
	}
}

func TestBuildElevationFlagsOutOfBounds(t *testing.T) {
	sec := buildElevationSection(domainRack(5, 4), []domainHostT{
		{ID: 1, Name: "tall", RackID: 5, RackPosition: 3, UHeight: 5}, // 3..7 exceeds 4
	}, nil, testCatalog())
	if !sec.Occupants[0].OutOfBounds {
		t.Fatal("expected out-of-bounds flag")
	}
}

func TestBuildElevationIgnoresOtherRacks(t *testing.T) {
	sec := buildElevationSection(domainRack(5, 10), []domainHostT{
		{ID: 1, Name: "elsewhere", RackID: 99, RackPosition: 1, UHeight: 1},
	}, nil, testCatalog())
	if len(sec.Occupants) != 0 {
		t.Fatalf("occupants from other racks should be ignored, got %d", len(sec.Occupants))
	}
}

func domainRack(id int64, uHeight int) domain.Rack {
	return domain.Rack{ID: id, Name: "R", UHeight: uHeight}
}

type domainHostT = domain.Host

// testCatalog is an empty entityCatalog; buildElevationSection only uses
// cat.path, which falls back to "/<type>s/<id>" for unknown types — fine here.
func testCatalog() entityCatalog { return entityCatalog{} }
