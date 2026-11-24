package discovery

import (
	"github.com/Dealisto/almanaut/internal/domain"
	"testing"
)

func TestMatchCandidatesUsesIdentityAcrossRenamesAndSources(t *testing.T) {
	hosts := []domain.Host{{ID: 1, Name: "manual rename", Type: "vm"}, {ID: 2, Name: "same-name", Type: "vm"}, {ID: 3, Name: "manual", Type: "physical"}}
	bindings := []domain.DiscoveryBinding{{Source: "xen-orchestra:one", ExternalID: "vm-1", EntityType: "host", EntityID: 1}, {Source: "xen-orchestra:other", ExternalID: "vm-2", EntityType: "host", EntityID: 2}}
	candidates := []Candidate{{ID: "vm-1", EntityType: "host", Host: domain.Host{Name: "source rename", Type: "vm"}}, {ID: "vm-2", EntityType: "host", Host: domain.Host{Name: "same-name", Type: "vm"}}, {ID: "host-1", EntityType: "host", Host: domain.Host{Name: " MANUAL ", Type: "physical"}}}
	got := MatchCandidates("xen-orchestra:one", candidates, hosts, nil, bindings)
	if got[0].ExistingID != 1 || !got[0].Bound || got[0].Ambiguous {
		t.Fatalf("rename lost identity: %+v", got[0])
	}
	if got[1].ExistingID != 0 || got[1].Ambiguous {
		t.Fatalf("other source suppressed new resource: %+v", got[1])
	}
	if got[2].ExistingID != 3 || got[2].Bound || got[2].Ambiguous {
		t.Fatalf("manual record not adoptable: %+v", got[2])
	}
}

func TestMatchCandidatesRejectsAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name       string
		candidates []Candidate
		hosts      []domain.Host
		bindings   []domain.DiscoveryBinding
	}{
		{name: "duplicate source names", hosts: []domain.Host{{ID: 1, Name: "same", Type: "vm"}}, candidates: []Candidate{{ID: "1", EntityType: "host", Host: domain.Host{Name: "same", Type: "vm"}}, {ID: "2", EntityType: "host", Host: domain.Host{Name: "SAME", Type: "vm"}}}},
		{name: "duplicate manual names", candidates: []Candidate{{ID: "1", EntityType: "host", Host: domain.Host{Name: "same", Type: "vm"}}}, hosts: []domain.Host{{ID: 1, Name: "same", Type: "vm"}, {ID: 2, Name: "same", Type: "vm"}}},
		{name: "wrong host type", candidates: []Candidate{{ID: "1", EntityType: "host", Host: domain.Host{Name: "same", Type: "vm"}}}, hosts: []domain.Host{{ID: 1, Name: "same", Type: "physical"}}},
		{name: "duplicate ids", candidates: []Candidate{{ID: "1", EntityType: "host", Host: domain.Host{Name: "a"}}, {ID: "1", EntityType: "host", Host: domain.Host{Name: "b"}}}},
		{name: "missing binding target", candidates: []Candidate{{ID: "1", EntityType: "host", Host: domain.Host{Name: "same", Type: "vm"}}}, bindings: []domain.DiscoveryBinding{{Source: "proxmox:default", ExternalID: "1", EntityType: "host", EntityID: 99}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, m := range MatchCandidates("proxmox:default", tc.candidates, tc.hosts, nil, tc.bindings) {
				if !m.Ambiguous {
					t.Fatalf("unsafe match: %+v", m)
				}
			}
		})
	}
}

func TestMatchCandidatesServicesAndBoundNameCollisions(t *testing.T) {
	candidates := []Candidate{{ID: "a", EntityType: "service", Service: domain.Service{Name: "same"}}, {ID: "b", EntityType: "service", Service: domain.Service{Name: "same"}}}
	services := []domain.Service{{ID: 1, Name: "renamed"}}
	bindings := []domain.DiscoveryBinding{{Source: "podman:lab", ExternalID: "a", EntityType: "service", EntityID: 1}}
	got := MatchCandidates("podman:lab", candidates, nil, services, bindings)
	if !got[0].Bound || got[0].ExistingID != 1 || got[0].Ambiguous {
		t.Fatalf("bound identity must win over source name collisions: %+v", got[0])
	}
	if got[1].Ambiguous || got[1].ExistingID != 0 {
		t.Fatal("distinct external identity without a manual match must remain importable")
	}
	manual := MatchCandidates("podman:lab", []Candidate{{ID: "c", EntityType: "service", Service: domain.Service{Name: " RENAMED "}}}, nil, services, nil)
	if manual[0].ExistingID != 1 || manual[0].Bound {
		t.Fatalf("manual service adoption: %+v", manual[0])
	}
}

func TestMatchCandidatesRecreatedContainerDoesNotDuplicateLinkedService(t *testing.T) {
	services := []domain.Service{{ID: 1, Name: "app"}}
	bindings := []domain.DiscoveryBinding{{Source: "podman:lab", ExternalID: "old-container", EntityType: "service", EntityID: 1}}
	candidates := []Candidate{{ID: "new-container", EntityType: "service", Service: domain.Service{Name: "app"}}}
	got := MatchCandidates("podman:lab", candidates, nil, services, bindings)
	if !got[0].Ambiguous || got[0].ExistingID != 0 {
		t.Fatalf("replacement identity must require review: %+v", got[0])
	}
	other := MatchCandidates("podman:other", candidates, nil, services, bindings)
	if other[0].Ambiguous || other[0].ExistingID != 0 {
		t.Fatalf("different source must remain independent: %+v", other[0])
	}
}
