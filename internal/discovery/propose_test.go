package discovery

import (
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestProposeServicesMapping(t *testing.T) {
	containers := []Container{{
		ID: "abc", Name: "jellyfin", Image: "jellyfin/jellyfin:latest",
		ComposeProject: "media",
		Ports:          []Port{{Public: 8096, Private: 8096, Proto: "tcp"}},
	}}
	got := ProposeServices(containers, nil)
	if len(got) != 1 {
		t.Fatalf("got %d proposals, want 1", len(got))
	}
	p := got[0]
	if p.ContainerID != "abc" {
		t.Errorf("ContainerID = %q", p.ContainerID)
	}
	if p.Service.Name != "jellyfin" || p.Service.Kind != "container" {
		t.Errorf("Service = %+v", p.Service)
	}
	if p.Service.Ports != "8096:8096/tcp" {
		t.Errorf("Ports = %q, want 8096:8096/tcp", p.Service.Ports)
	}
	if p.Service.Category != "media" {
		t.Errorf("Category = %q, want media", p.Service.Category)
	}
	if !strings.Contains(p.Service.Notes, "jellyfin/jellyfin:latest") {
		t.Errorf("Notes = %q, want image provenance", p.Service.Notes)
	}
	if p.AlreadyTracked {
		t.Error("AlreadyTracked = true, want false")
	}
}

func TestProposeServicesAlreadyTrackedCaseInsensitive(t *testing.T) {
	containers := []Container{{ID: "a", Name: "Jellyfin"}}
	existing := []domain.Service{{Name: "jellyfin"}}
	got := ProposeServices(containers, existing)
	if !got[0].AlreadyTracked {
		t.Error("expected case-insensitive already-tracked match")
	}
}

func TestProposeServicesEmptyPorts(t *testing.T) {
	got := ProposeServices([]Container{{ID: "a", Name: "x"}}, nil)
	if got[0].Service.Ports != "" {
		t.Errorf("Ports = %q, want empty", got[0].Service.Ports)
	}
}

func TestProposeServicesSortedByName(t *testing.T) {
	got := ProposeServices([]Container{
		{ID: "1", Name: "zeta"}, {ID: "2", Name: "alpha"},
	}, nil)
	if got[0].Service.Name != "alpha" || got[1].Service.Name != "zeta" {
		t.Errorf("not sorted by name: %q, %q", got[0].Service.Name, got[1].Service.Name)
	}
}
