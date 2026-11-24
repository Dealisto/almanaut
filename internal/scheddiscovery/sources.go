package scheddiscovery

import (
	"context"
	"fmt"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/notify"
	"github.com/Dealisto/almanaut/internal/store"
)

// Source identifies one configured engine independently of its display name.
type Source struct {
	Key, Kind    string
	Docker       DockerScanner
	Proxmox      ProxmoxScanner
	XenOrchestra XenOrchestraScanner
}

type BindingLister interface {
	List() ([]domain.DiscoveryBinding, error)
}

type ObservationRecorder interface {
	RecordBound(source string, ids []string, observedAt time.Time) error
}

// SourceDetector shares matching rules with manual imports and stores a separate
// history baseline for every source. Scheduled runs never create bindings.
type SourceDetector struct {
	source       Source
	detector     *Detector
	bindings     BindingLister
	observations ObservationRecorder
}

func NewSource(source Source, services ServiceLister, hosts HostLister, bindings BindingLister, runs *store.DiscoveryRunRepo, sender notify.Sender, observations ...ObservationRecorder) *SourceDetector {
	detector := &SourceDetector{source: source, detector: New(nil, nil, "", nil, services, hosts, runs, sender, nil, nil), bindings: bindings}
	if len(observations) > 0 {
		detector.observations = observations[0]
	}
	return detector
}

func (s *SourceDetector) Run(ctx context.Context) error {
	start := s.detector.now()
	candidates, err := s.candidates(ctx)
	var newKeys, observedIDs []string
	found := len(candidates)
	if err == nil {
		var hosts []domain.Host
		var services []domain.Service
		if s.source.Kind == "docker" || s.source.Kind == "podman" {
			services, err = s.detector.services.List()
		} else {
			hosts, err = s.detector.hosts.List()
		}
		if err == nil {
			var bindings []domain.DiscoveryBinding
			bindings, err = s.bindings.List()
			if err == nil {
				for _, m := range discovery.MatchCandidates(s.source.Key, candidates, hosts, services, bindings) {
					if m.Bound && !m.Ambiguous {
						observedIDs = append(observedIDs, m.ID)
					}
					if !m.Ambiguous && m.ExistingID == 0 {
						newKeys = append(newKeys, m.ID)
					}
				}
			}
		}
	}
	if err == nil && s.observations != nil {
		err = s.observations.RecordBound(s.source.Key, observedIDs, start)
	}
	return s.detector.finish(ctx, s.source.Key, start, found, newKeys, err)
}

func (s *SourceDetector) candidates(ctx context.Context) ([]discovery.Candidate, error) {
	var out []discovery.Candidate
	switch s.source.Kind {
	case "docker", "podman":
		if s.source.Docker == nil {
			break
		}
		resources, err := s.source.Docker.Containers(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range discovery.ProposeServices(resources, nil) {
			out = append(out, discovery.Candidate{ID: p.ContainerID, Service: p.Service, EntityType: "service"})
		}
		return out, nil
	case "proxmox":
		if s.source.Proxmox == nil {
			break
		}
		resources, err := s.source.Proxmox.Resources(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range discovery.ProposeProxmoxHosts(resources, nil) {
			out = append(out, discovery.Candidate{ID: p.ID, Host: p.Host, EntityType: "host"})
		}
		return out, nil
	case "xen-orchestra":
		if s.source.XenOrchestra == nil {
			break
		}
		resources, err := s.source.XenOrchestra.Resources(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range discovery.ProposeXenOrchestraHosts(resources, nil) {
			out = append(out, discovery.Candidate{ID: p.ID, ParentID: p.HostID, Host: p.Host, EntityType: "host"})
		}
		return out, nil
	}
	return nil, fmt.Errorf("discovery source has no supported scanner")
}
