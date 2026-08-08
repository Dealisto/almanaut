package web

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Dealisto/almanaut/internal/agentapi"
	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// agentDiskView is one disk from the agent's latest report, formatted for
// the host detail page.
type agentDiskView struct {
	Device, Model, Mount, Size, Used string
}

// agentIfaceView is one network interface from the agent's latest report,
// formatted for the host detail page.
type agentIfaceView struct {
	Name, MAC, Addrs string
}

// duplicateHostView is another host record that may track the same machine
// as this one.
type duplicateHostView struct {
	ID                int64
	Name, Reason, URL string
}

// agentSection is the host detail page's agent panel: whether the host has a
// reporting agent, its latest report (when one parsed), any outstanding
// clone conflict, and any other host record that looks like the same
// machine.
type agentSection struct {
	Bound                        bool
	AgentID, LastSeen            string
	AgentVersion, Kernel, Uptime string
	Disks                        []agentDiskView
	Interfaces                   []agentIfaceView
	ConflictAt, ConflictHostname string
	Duplicates                   []duplicateHostView
}

// agentSectionFor builds the hook closure for the host resource's agent
// panel, matching the shape of certProbeSection: a builder returning a
// closure over its repos.
func agentSectionFor(agents *store.AgentRepo, hosts *store.HostRepo, cat entityCatalog) func(domain.Host) *agentSection {
	return func(h domain.Host) *agentSection {
		sec := &agentSection{}

		// ErrNotFound means the host has no agent, which is the normal,
		// common case. Any other error is a database hiccup: rather than
		// fail the whole host page over it, fall back to the same
		// unbound-looking section — "no agent" is a survivable inaccuracy,
		// a 500 is not.
		if binding, err := agents.ByHostID(h.ID); err == nil {
			sec.Bound = true
			sec.AgentID = binding.AgentID
			sec.LastSeen = binding.LastSeen
			sec.ConflictAt = binding.LastConflictAt
			sec.ConflictHostname = binding.LastConflictHostname
			fillReport(sec, agents, h.ID)
		}

		sec.Duplicates = duplicatesOf(h, agents, hosts, cat)
		return sec
	}
}

// fillReport loads hostID's latest agent report into sec. A missing report
// or one that fails to unmarshal leaves the report fields empty rather than
// failing the page: the binding facts filled in by the caller are still
// worth showing.
func fillReport(sec *agentSection, agents *store.AgentRepo, hostID int64) {
	payload, err := agents.LatestReport(hostID)
	if err != nil {
		return
	}
	var rep agentapi.Report
	if err := json.Unmarshal([]byte(payload), &rep); err != nil {
		return
	}
	sec.AgentVersion = rep.AgentVersion
	sec.Kernel = rep.Kernel
	sec.Uptime = humanizeUptime(rep.Uptime)
	for _, d := range rep.Disks {
		sec.Disks = append(sec.Disks, agentDiskView{
			Device: d.Device,
			Model:  d.Model,
			Mount:  d.Mount,
			Size:   humanizeBytes(d.SizeBytes),
			Used:   humanizeBytes(d.UsedBytes),
		})
	}
	for _, i := range rep.Interfaces {
		sec.Interfaces = append(sec.Interfaces, agentIfaceView{
			Name:  i.Name,
			MAC:   i.MAC,
			Addrs: strings.Join(i.Addrs, ", "),
		})
	}
}

// duplicatesOf finds other host records that look like the same machine as
// h: a normalized name match (the same rule discovery.NormalizeName applies
// during adoption, so the panel agrees with the server) or a shared IP. A
// match is only surfaced when h or the other host has an agent binding —
// two hand-entered hosts sharing a name are the operator's business, not the
// agent's. Any repo error degrades to no duplicates rather than failing the
// page.
func duplicatesOf(h domain.Host, agents *store.AgentRepo, hosts *store.HostRepo, cat entityCatalog) []duplicateHostView {
	all, err := hosts.List()
	if err != nil {
		return nil
	}
	bound, err := agents.BoundHostIDs()
	if err != nil {
		return nil
	}
	name := discovery.NormalizeName(h.Name)
	ips := make(map[string]bool, len(h.IPs))
	for _, ip := range h.IPs {
		ips[ip] = true
	}

	var out []duplicateHostView
	for _, other := range all {
		if other.ID == h.ID {
			continue
		}
		if !bound[h.ID] && !bound[other.ID] {
			continue
		}
		reason := ""
		switch {
		case discovery.NormalizeName(other.Name) == name:
			reason = "same name"
		case sharedIP(ips, other.IPs) != "":
			reason = "shares " + sharedIP(ips, other.IPs)
		default:
			continue
		}
		out = append(out, duplicateHostView{
			ID:     other.ID,
			Name:   other.Name,
			Reason: reason,
			URL:    cat.path("host", other.ID),
		})
	}
	return out
}

// sharedIP returns the first address in otherIPs that is also in ips, or ""
// when there is no overlap.
func sharedIP(ips map[string]bool, otherIPs []string) string {
	for _, ip := range otherIPs {
		if ips[ip] {
			return ip
		}
	}
	return ""
}

// humanizeUptime renders a duration in seconds as "N day(s), N hour(s),
// N minute(s)", omitting the leading units that are zero. Seconds are
// dropped: the panel is for a glance at how long the machine has been up,
// not a precise timer. A non-positive value (unknown, or the agent hasn't
// reported one) yields "".
func humanizeUptime(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", days, pluralize("day", days)))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", hours, pluralize("hour", hours)))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d %s", minutes, pluralize("minute", minutes)))
	}
	return strings.Join(parts, ", ")
}

// pluralize appends "s" to unit unless n is exactly 1.
func pluralize(unit string, n int64) string {
	if n == 1 {
		return unit
	}
	return unit + "s"
}
