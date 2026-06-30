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
		tracked[NormalizeName(s.Name)] = true
	}
	out := make([]Proposal, 0, len(containers))
	for _, c := range containers {
		out = append(out, Proposal{
			ContainerID:    c.ID,
			Service:        serviceFromContainer(c),
			AlreadyTracked: tracked[NormalizeName(c.Name)],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Service.Name != out[j].Service.Name {
			return out[i].Service.Name < out[j].Service.Name
		}
		return out[i].ContainerID < out[j].ContainerID // stable order for duplicate names
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

// NormalizeName canonicalizes an entity name for case-insensitive,
// whitespace-insensitive matching (e.g. comparing a discovered name against an
// existing one). It is exported so the import handlers match names the same way
// the proposal step does.
func NormalizeName(s string) string {
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

// ProxmoxProposal is a discovered Proxmox resource mapped to a candidate Host,
// with a flag indicating whether a host with the same name already exists.
type ProxmoxProposal struct {
	ID             string // Proxmox resource id, e.g. "qemu/100" (import key)
	Host           domain.Host
	AlreadyTracked bool
}

// ProposeProxmoxHosts maps Proxmox nodes/VMs/containers to candidate Hosts,
// nodes first then by name, marking those whose name already matches an
// existing host (case-insensitive).
func ProposeProxmoxHosts(res []ProxmoxResource, existing []domain.Host) []ProxmoxProposal {
	tracked := make(map[string]bool, len(existing))
	for _, h := range existing {
		tracked[NormalizeName(h.Name)] = true
	}
	out := make([]ProxmoxProposal, 0, len(res))
	for _, r := range res {
		h := hostFromResource(r)
		out = append(out, ProxmoxProposal{
			ID:             r.ID,
			Host:           h,
			AlreadyTracked: tracked[NormalizeName(h.Name)],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ni := out[i].Host.Type == "physical"
		nj := out[j].Host.Type == "physical"
		if ni != nj {
			return ni // nodes first
		}
		if ai, aj := NormalizeName(out[i].Host.Name), NormalizeName(out[j].Host.Name); ai != aj {
			return ai < aj
		}
		return out[i].ID < out[j].ID // stable order for duplicate names (ID is unique)
	})
	return out
}

func hostFromResource(r ProxmoxResource) domain.Host {
	name := r.Name
	if strings.TrimSpace(name) == "" {
		name = r.Node // node rows carry the name in Node
	}
	h := domain.Host{
		Name:   name,
		Type:   proxmoxHostType(r.Type),
		Status: r.Status,
		Notes:  "Discovered from Proxmox.",
	}
	if r.MaxCPU > 0 {
		h.CPU = fmt.Sprintf("%d cores", r.MaxCPU)
	}
	if r.MaxMem > 0 {
		h.RAM = humanBytes(r.MaxMem)
	}
	if r.MaxDisk > 0 {
		h.Disk = humanBytes(r.MaxDisk)
	}
	return h
}

func proxmoxHostType(t string) string {
	switch t {
	case "qemu":
		return "vm"
	case "lxc":
		return "lxc"
	default:
		return "physical"
	}
}

// humanBytes renders a byte count in binary units (e.g. "4.0 GiB").
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
