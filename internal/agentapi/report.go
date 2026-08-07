// Package agentapi defines the wire format shared by the almanaut server and
// the on-host inventory agent. Both binaries import this package, so the
// protocol cannot drift between them.
package agentapi

import (
	"fmt"
	"sort"
	"strings"
)

// SchemaVersion is the wire format version. The server refuses a report whose
// SchemaVersion it does not know rather than guessing at unfamiliar fields.
const SchemaVersion = 1

// Interface is one network interface as seen by the agent.
type Interface struct {
	Name  string   `json:"name"`
	MAC   string   `json:"mac"`
	Addrs []string `json:"addrs"`
}

// Disk is one block device (bare metal / VM) or one mounted filesystem
// (container, where enumerating physical devices is meaningless).
type Disk struct {
	Device     string `json:"device"`
	Model      string `json:"model"`
	Filesystem string `json:"filesystem"`
	Mount      string `json:"mount"`
	SizeBytes  int64  `json:"size_bytes"`
	UsedBytes  int64  `json:"used_bytes"`
}

// Report is one agent check-in. The scalar summary fields (OS, CPU, RAM, Disk)
// are what the server merges into domain.Host; the structured fields are kept
// verbatim in agent_reports for display.
//
// An empty scalar means "could not determine", never "empty". The server's
// merge relies on that distinction, so collectors must leave a field blank
// rather than invent a zero value.
type Report struct {
	SchemaVersion int    `json:"schema_version"`
	AgentID       string `json:"agent_id"`
	AgentVersion  string `json:"agent_version"`

	Hostname string `json:"hostname"`
	VirtKind string `json:"virt_kind"` // physical | vm | lxc
	OS       string `json:"os"`
	Kernel   string `json:"kernel"`
	CPU      string `json:"cpu"`
	RAM      string `json:"ram"`
	Disk     string `json:"disk"`
	Uptime   int64  `json:"uptime_seconds"`

	Interfaces []Interface `json:"interfaces"`
	Disks      []Disk      `json:"disks"`
}

// Validate checks the fields the server cannot proceed without.
func (r Report) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d (server speaks %d)", r.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(r.AgentID) == "" {
		return fmt.Errorf("agent_id is required")
	}
	if strings.TrimSpace(r.Hostname) == "" {
		return fmt.Errorf("hostname is required")
	}
	return nil
}

// Fingerprint is the clone-detection key: hostname plus every MAC, lowercased
// and sorted so that interface enumeration order (which is not stable across
// reboots) cannot make one machine look like two.
func (r Report) Fingerprint() string {
	macs := make([]string, 0, len(r.Interfaces))
	for _, i := range r.Interfaces {
		if m := strings.ToLower(strings.TrimSpace(i.MAC)); m != "" {
			macs = append(macs, m)
		}
	}
	sort.Strings(macs)
	return strings.ToLower(strings.TrimSpace(r.Hostname)) + "|" + strings.Join(macs, ",")
}

// ReportedIPs flattens every interface address, in interface order.
func (r Report) ReportedIPs() []string {
	out := []string{}
	for _, i := range r.Interfaces {
		for _, a := range i.Addrs {
			if a = strings.TrimSpace(a); a != "" {
				out = append(out, a)
			}
		}
	}
	return out
}
