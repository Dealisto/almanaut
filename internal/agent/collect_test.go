package agent

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
)

func TestCollectFillsAReportableStructure(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"etc/os-release":              "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\n",
		"proc/sys/kernel/osrelease":   "6.1.0-18-amd64\n",
		"proc/uptime":                 "12345.67 0\n",
		"proc/meminfo":                "MemTotal:       65805304 kB\n",
		"proc/cpuinfo":                cpuinfo4Core,
		"sys/block/nvme0n1/size":      "1953525168\n",
		"sys/class/dmi/id/sys_vendor": "ASUSTeK COMPUTER INC.\n",
	})
	rep := Collect(CollectOptions{
		Root:         r,
		AgentID:      "11111111-2222-4333-8444-555555555555",
		AgentVersion: "0.1.0",
		Hostname:     "nas01",
		Interfaces: fakeLister(
			NetInterface{Name: "eth0", MAC: "aa:bb:cc:00:00:01", Addrs: []string{"192.168.1.10/24"}},
		),
	})

	if rep.SchemaVersion != agentapi.SchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", rep.SchemaVersion, agentapi.SchemaVersion)
	}
	if err := rep.Validate(); err != nil {
		t.Fatalf("assembled report is invalid: %v", err)
	}
	if rep.Hostname != "nas01" || rep.AgentID == "" || rep.AgentVersion != "0.1.0" {
		t.Fatalf("identity fields = %+v", rep)
	}
	if rep.VirtKind != "physical" {
		t.Fatalf("VirtKind = %q, want physical", rep.VirtKind)
	}
	if rep.OS == "" || rep.Kernel == "" || rep.CPU == "" || rep.RAM == "" || rep.Disk == "" {
		t.Fatalf("a scalar field is unexpectedly empty: %+v", rep)
	}
	if len(rep.Interfaces) != 1 || len(rep.Disks) != 1 {
		t.Fatalf("structured fields = %+v", rep)
	}
	// agentapi is the single definition of a reportable address.
	if ips := rep.ReportedIPs(); len(ips) != 1 || ips[0] != "192.168.1.10" {
		t.Fatalf("ReportedIPs = %v, want the sanitized address", ips)
	}
}

// A container must not describe the host's disks. It also proves the
// inContainer decision is actually derived from the detected virt kind.
func TestCollectInContainerUsesRootFilesystem(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"proc/cpuinfo":             cpuinfo4Core,
		"sys/fs/cgroup/memory.max": "4294967296\n",
		"sys/block/nvme0n1/size":   "1953525168\n",
	})
	rep := Collect(CollectOptions{
		Root:       r,
		AgentID:    "id",
		Hostname:   "ct100",
		VirtKind:   "lxc",
		Interfaces: fakeLister(),
		Usage: func(string) (int64, int64, error) {
			return 34359738368, 19327352832, nil
		},
	})
	if rep.VirtKind != "lxc" {
		t.Fatalf("VirtKind = %q", rep.VirtKind)
	}
	if len(rep.Disks) != 1 || rep.Disks[0].Device != "rootfs" {
		t.Fatalf("disks = %+v, want the rootfs entry, not the host's nvme0n1", rep.Disks)
	}
	if rep.RAM != "4.0 GB" {
		t.Fatalf("RAM = %q, want the cgroup allocation", rep.RAM)
	}
}

// Nothing readable must still produce a structurally valid report: the server
// treats every empty field as "leave the existing value alone".
func TestCollectOnBarrenRootStillValidates(t *testing.T) {
	rep := Collect(CollectOptions{
		Root:       fixtureRoot(t, map[string]string{}),
		AgentID:    "id",
		Hostname:   "h",
		Interfaces: fakeLister(),
	})
	if err := rep.Validate(); err != nil {
		t.Fatalf("Validate = %v, want nil", err)
	}
	if rep.OS != "" || rep.CPU != "" || rep.RAM != "" || rep.Disk != "" {
		t.Fatalf("expected every undeterminable field to be empty: %+v", rep)
	}
}
