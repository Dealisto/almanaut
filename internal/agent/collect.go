package agent

import (
	"context"

	"github.com/Dealisto/almanaut/internal/agentapi"
)

// System calls such as statfs cannot be interrupted portably. Keep at most one
// collection worker alive even if a caller times out and retries. The one-shot
// CLI exits after a timeout, terminating its remaining worker with the process.
var collectionSlot = make(chan struct{}, 1)

// CollectContext bounds how long the caller waits for machine facts. A timed-out
// collection is discarded, so a partial or late report is never sent. The
// underlying OS call may finish later; it cannot spawn further workers on retry.
func CollectContext(ctx context.Context, opts CollectOptions) (agentapi.Report, error) {
	if err := ctx.Err(); err != nil {
		return agentapi.Report{}, err
	}
	select {
	case collectionSlot <- struct{}{}:
	case <-ctx.Done():
		return agentapi.Report{}, ctx.Err()
	}
	result := make(chan agentapi.Report, 1)
	go func() {
		defer func() { <-collectionSlot }()
		if ctx.Err() == nil {
			result <- Collect(opts)
		}
	}()
	select {
	case report := <-result:
		if err := ctx.Err(); err != nil {
			return agentapi.Report{}, err
		}
		return report, nil
	case <-ctx.Done():
		return agentapi.Report{}, ctx.Err()
	}
}

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

	diskSummary, disks := CollectDisks(opts.Root, virt, opts.Usage)

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
