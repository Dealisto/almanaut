package agent

import (
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
)

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

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
	if rep.Hostname != "nas01" || rep.AgentID != "11111111-2222-4333-8444-555555555555" || rep.AgentVersion != "0.1.0" {
		t.Fatalf("identity fields = %+v", rep)
	}
	if rep.VirtKind != "physical" {
		t.Fatalf("VirtKind = %q, want physical", rep.VirtKind)
	}
	// Assert actual values against fixture data, not just non-emptiness
	if rep.OS != "Debian GNU/Linux 12 (bookworm)" {
		t.Fatalf("OS = %q, want Debian GNU/Linux 12 (bookworm)", rep.OS)
	}
	if rep.Kernel != "6.1.0-18-amd64" {
		t.Fatalf("Kernel = %q, want 6.1.0-18-amd64", rep.Kernel)
	}
	if rep.CPU != "AMD Ryzen 9 5950X 16-Core Processor (4 cores)" {
		t.Fatalf("CPU = %q, want AMD Ryzen 9 5950X 16-Core Processor (4 cores)", rep.CPU)
	}
	if rep.RAM != "62.8 GB" {
		t.Fatalf("RAM = %q, want 62.8 GB", rep.RAM)
	}
	if rep.Uptime != 12345 {
		t.Fatalf("Uptime = %d, want 12345", rep.Uptime)
	}
	// Disk summary should contain the device name, preventing transposition
	if !contains(rep.Disk, "nvme0n1") {
		t.Fatalf("Disk = %q, want it to contain nvme0n1", rep.Disk)
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

// The inContainer decision must be derived from detected virtualization kind,
// not just from an explicit override. This test exercises the detection path
// where VirtKind is left empty and the fixture supplies a container marker.
func TestCollectDetectsLXCAndRoutesToRootfs(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"run/systemd/container":    "lxc\n",
		"proc/cpuinfo":             cpuinfo4Core,
		"sys/fs/cgroup/memory.max": "4294967296\n",
		"sys/block/nvme0n1/size":   "1953525168\n",
	})
	rep := Collect(CollectOptions{
		Root:       r,
		AgentID:    "id",
		Hostname:   "ct100",
		Interfaces: fakeLister(),
		Usage: func(string) (int64, int64, error) {
			return 34359738368, 19327352832, nil
		},
	})
	if rep.VirtKind != "lxc" {
		t.Fatalf("VirtKind = %q, want lxc (detected from container marker)", rep.VirtKind)
	}
	if len(rep.Disks) != 1 || rep.Disks[0].Device != "rootfs" {
		t.Fatalf("disks = %+v, want the rootfs entry (proves inContainer was derived from detection)", rep.Disks)
	}
}

// The defect this guards against: an LXC container running a non-systemd
// init (Alpine/OpenRC, common on Proxmox) has neither the systemd container
// marker nor a cgroup path naming the runtime — only /proc/1/environ says
// container=lxc. Detection must still catch it, and the disk collector must
// still refuse to enumerate the host's <sys>/block devices even though they
// are present in the fixture and would otherwise be read as this machine's own.
func TestCollectDetectsLXCViaEnvironAndAvoidsHostDisks(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"proc/1/cgroup":          "0::/init.scope\n",
		"proc/1/environ":         "PATH=/usr/bin\x00container=lxc\x00",
		"sys/block/nvme0n1/size": "1953525168\n",
	})
	rep := Collect(CollectOptions{
		Root:       r,
		AgentID:    "id",
		Hostname:   "ct-alpine",
		Interfaces: fakeLister(),
		Usage: func(string) (int64, int64, error) {
			return 34359738368, 19327352832, nil
		},
	})
	if rep.VirtKind != "lxc" {
		t.Fatalf("VirtKind = %q, want lxc (detected via /proc/1/environ)", rep.VirtKind)
	}
	if len(rep.Disks) != 1 || rep.Disks[0].Device != "rootfs" {
		t.Fatalf("disks = %+v, want the rootfs entry, not the host's nvme0n1", rep.Disks)
	}
	if contains(rep.Disk, "nvme0n1") {
		t.Fatalf("Disk summary %q leaks the host's block device", rep.Disk)
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
