package discovery

import (
	"fmt"
	"sort"

	"github.com/Dealisto/almanaut/internal/domain"
)

// XenOrchestraProposal maps an XO resource to a candidate inventory host.
// AmbiguousName prevents choosing arbitrarily between identically named objects.
type XenOrchestraProposal struct {
	ID, HostID     string
	Host           domain.Host
	AlreadyTracked bool
	AmbiguousName  bool
}

// ProposeXenOrchestraHosts preserves manual records by matching normalized names.
// Physical hosts come first so an import can link VMs to their selected hosts.
func ProposeXenOrchestraHosts(resources []XenOrchestraResource, existing []domain.Host) []XenOrchestraProposal {
	tracked := make(map[string]bool, len(existing))
	for _, h := range existing {
		tracked[NormalizeName(h.Name)] = true
	}
	names := make(map[string]int, len(resources))
	for _, r := range resources {
		names[NormalizeName(r.Name)]++
	}
	out := make([]XenOrchestraProposal, 0, len(resources))
	for _, r := range resources {
		kind := "vm"
		if r.Type == "host" {
			kind = "physical"
		}
		h := domain.Host{
			Name: r.Name, Type: kind, Status: r.Status,
			IPs: append([]string(nil), r.IPs...), Notes: "Discovered from Xen Orchestra. Resource ID: " + r.ID,
		}
		if r.CPU > 0 {
			h.CPU = fmt.Sprintf("%d cores", r.CPU)
		}
		if r.Memory > 0 {
			h.RAM = humanBytes(r.Memory)
		}
		out = append(out, XenOrchestraProposal{
			ID: r.ID, HostID: r.HostID, Host: h,
			AlreadyTracked: tracked[NormalizeName(h.Name)], AmbiguousName: names[NormalizeName(h.Name)] > 1,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if hi, hj := out[i].Host.Type == "physical", out[j].Host.Type == "physical"; hi != hj {
			return hi
		}
		if ni, nj := NormalizeName(out[i].Host.Name), NormalizeName(out[j].Host.Name); ni != nj {
			return ni < nj
		}
		return out[i].ID < out[j].ID
	})
	return out
}
