package discovery

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
)

// Proposal is a discovered container mapped to a candidate Service, with a flag
// indicating whether a service with the same name already exists.
type Proposal struct {
	ContainerID    string
	Service        domain.Service
	AlreadyTracked bool
}

// ProposeServices maps discovered containers to candidate Services, sorted by
// name, marking those whose name already matches an existing service
// (case-insensitive).
func ProposeServices(containers []Container, existing []domain.Service) []Proposal {
	tracked := make(map[string]bool, len(existing))
	for _, s := range existing {
		tracked[normalizeName(s.Name)] = true
	}
	out := make([]Proposal, 0, len(containers))
	for _, c := range containers {
		out = append(out, Proposal{
			ContainerID:    c.ID,
			Service:        serviceFromContainer(c),
			AlreadyTracked: tracked[normalizeName(c.Name)],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Service.Name < out[j].Service.Name
	})
	return out
}

func serviceFromContainer(c Container) domain.Service {
	return domain.Service{
		Name:     c.Name,
		Kind:     "container",
		Ports:    formatPorts(c.Ports),
		Category: c.ComposeProject,
		Notes:    provenance(c.Image),
	}
}

func formatPorts(ports []Port) string {
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d:%d/%s", p.Public, p.Private, p.Proto))
	}
	return strings.Join(parts, ", ")
}

func provenance(image string) string {
	if image == "" {
		return "Discovered from Docker."
	}
	return "Discovered from Docker. Image: " + image
}

func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// HostProposal is a scanned host mapped to a candidate Host, with a flag
// indicating whether one of its IPs already belongs to an existing host.
type HostProposal struct {
	IP             string
	Ports          string // open ports as "80, 443" (display + import transport)
	Host           domain.Host
	AlreadyTracked bool
}

// ProposeHosts maps scanned hosts to candidate Hosts, sorted by IP, marking
// those whose IP already belongs to an existing host. Host.Type is left empty;
// the import step sets it from the review form.
func ProposeHosts(scanned []ScannedHost, existing []domain.Host) []HostProposal {
	tracked := make(map[string]bool)
	for _, h := range existing {
		for _, ip := range h.IPs {
			tracked[strings.TrimSpace(ip)] = true
		}
	}
	out := make([]HostProposal, 0, len(scanned))
	for _, sh := range scanned {
		ports := formatPortsInts(sh.OpenPorts)
		name := sh.Hostname
		if name == "" {
			name = sh.IP
		}
		out = append(out, HostProposal{
			IP:    sh.IP,
			Ports: ports,
			Host: domain.Host{
				Name:  name,
				IPs:   []string{sh.IP},
				Notes: NetworkHostNotes(ports),
			},
			AlreadyTracked: tracked[sh.IP],
		})
	}
	sort.Slice(out, func(i, j int) bool { return ipLess(out[i].IP, out[j].IP) })
	return out
}

// NetworkHostNotes builds the provenance note for an imported host.
func NetworkHostNotes(ports string) string {
	if ports == "" {
		return "Discovered by network scan."
	}
	return "Discovered by network scan. Open ports: " + ports
}

func formatPortsInts(ports []int) string {
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d", p))
	}
	return strings.Join(parts, ", ")
}
