package agent

import "github.com/Dealisto/almanaut/internal/agentapi"

// CollectOptions are the inputs Collect cannot discover for itself.
//
// Hostname, Interfaces and Usage are injected rather than read from the
// process, so the whole assembly is deterministic under test. VirtKind may be
// set to override detection; leaving it empty lets Collect detect it.
type CollectOptions struct {
	Root         Root
	AgentID      string
	AgentVersion string
	Hostname     string
	VirtKind     string
	Interfaces   InterfaceLister
	Usage        func(string) (int64, int64, error)
}

// Collect assembles one report. It never fails: a field the machine will not
// reveal is left empty, which is the wire signal for "could not determine" and
// makes the server preserve whatever the record already holds.
func Collect(opts CollectOptions) agentapi.Report {
	facts := CollectHostFacts(opts.Root)
	virt := opts.VirtKind
	if virt == "" {
		virt = facts.VirtKind
	}

	diskSummary, disks := CollectDisks(opts.Root, virt == "lxc", opts.Usage)

	return agentapi.Report{
		SchemaVersion: agentapi.SchemaVersion,
		AgentID:       opts.AgentID,
		AgentVersion:  opts.AgentVersion,
		Hostname:      opts.Hostname,
		VirtKind:      virt,
		OS:            facts.OS,
		Kernel:        facts.Kernel,
		CPU:           CollectCPU(opts.Root),
		RAM:           CollectRAM(opts.Root),
		Disk:          diskSummary,
		Uptime:        facts.UptimeSeconds,
		Interfaces:    CollectInterfaces(opts.Interfaces),
		Disks:         disks,
	}
}
