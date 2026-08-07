package web

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
	"github.com/Dealisto/almanaut/internal/domain"
)

func TestMergeAgentReportOverwritesOwnedFieldsOnly(t *testing.T) {
	existing := domain.Host{
		ID: 5, Name: "nas-principal", Type: "vps",
		OS: "old os", CPU: "old cpu", RAM: "8 GB", Disk: "old disk",
		IPs: []string{"10.0.0.1"}, Notes: "hand written", Status: "active",
		CheckAddress: "10.0.0.1:22", RackID: 2, RackPosition: 4, UHeight: 1,
	}
	r := agentapi.Report{
		Hostname: "truenas01", VirtKind: "vm",
		OS: "Debian 12", CPU: "Ryzen 5950X (16c/32t)", RAM: "64 GB", Disk: "2x 1 TB NVMe",
		Interfaces: []agentapi.Interface{{Name: "eth0", Addrs: []string{"192.168.1.10"}}},
	}

	got := mergeAgentReport(existing, r, false)

	if got.OS != "Debian 12" || got.CPU != "Ryzen 5950X (16c/32t)" || got.RAM != "64 GB" || got.Disk != "2x 1 TB NVMe" {
		t.Fatalf("agent-owned fields not applied: %+v", got)
	}
	if len(got.IPs) != 1 || got.IPs[0] != "192.168.1.10" {
		t.Fatalf("IPs = %v, want the reported set", got.IPs)
	}
	// Human-authored fields are untouched — including Name, even though the
	// reported hostname differs.
	if got.Name != "nas-principal" {
		t.Fatalf("Name = %q, want it untouched", got.Name)
	}
	if got.Notes != "hand written" || got.Status != "active" || got.CheckAddress != "10.0.0.1:22" {
		t.Fatalf("human fields clobbered: %+v", got)
	}
	if got.RackID != 2 || got.RackPosition != 4 || got.UHeight != 1 {
		t.Fatalf("rack placement clobbered: %+v", got)
	}
	if got.ID != 5 {
		t.Fatalf("ID = %d, want 5", got.ID)
	}
}

// Type is a commercial distinction for "vps", which the agent cannot observe.
// Overwriting it on update would demote every vps to vm on the next run.
func TestMergeAgentReportNeverChangesTypeOnUpdate(t *testing.T) {
	got := mergeAgentReport(domain.Host{Type: "vps"}, agentapi.Report{VirtKind: "vm", Hostname: "h"}, false)
	if got.Type != "vps" {
		t.Fatalf("Type = %q, want vps preserved", got.Type)
	}
}

func TestMergeAgentReportSetsTypeOnCreate(t *testing.T) {
	got := mergeAgentReport(domain.Host{}, agentapi.Report{VirtKind: "lxc", Hostname: "ct"}, true)
	if got.Type != "lxc" {
		t.Fatalf("Type = %q, want lxc", got.Type)
	}
	// An unknown or absent virt kind must still produce a valid Host.
	got = mergeAgentReport(domain.Host{}, agentapi.Report{VirtKind: "", Hostname: "ct"}, true)
	if got.Type != "physical" {
		t.Fatalf("Type = %q, want physical fallback", got.Type)
	}
}

// The rule that makes LXC uncertainty safe: a collector that could not
// determine a value sends "", and "" must never erase what a human entered.
func TestMergeAgentReportEmptyNeverOverwrites(t *testing.T) {
	existing := domain.Host{OS: "Proxmox 8", CPU: "Xeon", RAM: "32 GB", Disk: "4 TB", IPs: []string{"10.0.0.9"}}
	got := mergeAgentReport(existing, agentapi.Report{Hostname: "pve"}, false)
	if got.OS != "Proxmox 8" || got.CPU != "Xeon" || got.RAM != "32 GB" || got.Disk != "4 TB" {
		t.Fatalf("empty report erased existing values: %+v", got)
	}
	if len(got.IPs) != 1 || got.IPs[0] != "10.0.0.9" {
		t.Fatalf("IPs = %v, want preserved", got.IPs)
	}
}

// On create the host must be named after the reported hostname — there is no
// human-chosen name to protect yet.
func TestMergeAgentReportNamesNewHostFromHostname(t *testing.T) {
	got := mergeAgentReport(domain.Host{}, agentapi.Report{Hostname: "web03", VirtKind: "vm"}, true)
	if got.Name != "web03" {
		t.Fatalf("Name = %q, want web03", got.Name)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("merged host is invalid: %v", err)
	}
}
