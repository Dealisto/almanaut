package web

import (
	"github.com/Dealisto/almanaut/internal/agentapi"
	"github.com/Dealisto/almanaut/internal/domain"
)

// agentVirtToHostType maps a reported virtualization kind onto domain.HostTypes.
// "vps" is absent on purpose: it is a commercial distinction the agent cannot
// observe, since a VPS presents itself as a VM.
var agentVirtToHostType = map[string]string{
	"physical": "physical",
	"vm":       "vm",
	"kvm":      "vm",
	"lxc":      "lxc",
}

// mergeAgentReport returns h with the agent-owned fields replaced by r's.
//
// Two rules keep this safe. First, an empty reported value never overwrites an
// existing one: a collector that could not determine a value (the common case
// inside an LXC without lxcfs) sends "", and absence of data is not data
// asserting emptiness. Second, Type is set only at creation — on update a
// hand-set "vps" must survive, or every run would demote it to "vm".
//
// Everything not listed here is human-authored and deliberately untouched,
// including Name: a record deliberately called "nas-principal" must not be
// renamed to whatever the machine's hostname happens to be.
func mergeAgentReport(h domain.Host, r agentapi.Report, isCreate bool) domain.Host {
	h.OS = preferReported(r.OS, h.OS)
	h.CPU = preferReported(r.CPU, h.CPU)
	h.RAM = preferReported(r.RAM, h.RAM)
	h.Disk = preferReported(r.Disk, h.Disk)
	if ips := r.ReportedIPs(); len(ips) > 0 {
		h.IPs = ips
	}
	if isCreate {
		h.Name = r.Hostname
		t, ok := agentVirtToHostType[r.VirtKind]
		if !ok {
			t = "physical" // domain.Host.Validate rejects anything outside HostTypes
		}
		h.Type = t
	}
	return h
}

// preferReported returns reported when it carries a value, else current.
func preferReported(reported, current string) string {
	if reported == "" {
		return current
	}
	return reported
}
