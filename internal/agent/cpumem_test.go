package agent

import "testing"

const cpuinfo4Core = `processor	: 0
model name	: AMD Ryzen 9 5950X 16-Core Processor
processor	: 1
model name	: AMD Ryzen 9 5950X 16-Core Processor
processor	: 2
model name	: AMD Ryzen 9 5950X 16-Core Processor
processor	: 3
model name	: AMD Ryzen 9 5950X 16-Core Processor
`

func TestCollectRAMFromProcMeminfoOnBareMetal(t *testing.T) {
	// No cgroup limit files at all: the v2 root cgroup has no memory.max.
	r := fixtureRoot(t, map[string]string{
		"proc/meminfo": "MemTotal:       65805304 kB\nMemFree:         1234 kB\n",
	})
	if got := CollectRAM(r); got != "62.8 GB" {
		t.Fatalf("CollectRAM = %q, want 62.8 GB", got)
	}
}

// The case the whole design exists for: lxcfs is absent so /proc/meminfo shows
// the host's 64 GB, but the cgroup says this container was given 4 GB.
func TestCollectRAMPrefersCgroupLimitOverProc(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"proc/meminfo":             "MemTotal:       65805304 kB\n",
		"sys/fs/cgroup/memory.max": "4294967296\n",
	})
	if got := CollectRAM(r); got != "4.0 GB" {
		t.Fatalf("CollectRAM = %q, want 4.0 GB (the cgroup limit, not the host's)", got)
	}
}

// "max" means the cgroup imposes no limit, so /proc is the truth.
func TestCollectRAMFallsBackWhenCgroupUnlimited(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"proc/meminfo":             "MemTotal:       65805304 kB\n",
		"sys/fs/cgroup/memory.max": "max\n",
	})
	if got := CollectRAM(r); got != "62.8 GB" {
		t.Fatalf("CollectRAM = %q, want the /proc value", got)
	}
}

func TestCollectRAMReadsCgroupV1(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"proc/meminfo": "MemTotal:       65805304 kB\n",
		"sys/fs/cgroup/memory/memory.limit_in_bytes": "2147483648\n",
	})
	if got := CollectRAM(r); got != "2.0 GB" {
		t.Fatalf("CollectRAM = %q, want 2.0 GB", got)
	}
}

// cgroup v1 spells "unlimited" as a huge sentinel rather than a word.
func TestCollectRAMTreatsV1SentinelAsUnlimited(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"proc/meminfo": "MemTotal:       65805304 kB\n",
		"sys/fs/cgroup/memory/memory.limit_in_bytes": "9223372036854771712\n",
	})
	if got := CollectRAM(r); got != "62.8 GB" {
		t.Fatalf("CollectRAM = %q, want the /proc value", got)
	}
}

func TestCollectRAMEmptyWhenNothingReadable(t *testing.T) {
	if got := CollectRAM(fixtureRoot(t, map[string]string{})); got != "" {
		t.Fatalf("CollectRAM = %q, want empty so the server keeps the existing value", got)
	}
}

func TestCollectCPUUsesModelAndCgroupCoreCount(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"proc/cpuinfo":                        cpuinfo4Core,
		"sys/fs/cgroup/cpuset.cpus.effective": "0-1\n",
	})
	want := "AMD Ryzen 9 5950X 16-Core Processor (2 cores allocated)"
	if got := CollectCPU(r); got != want {
		t.Fatalf("CollectCPU = %q, want %q", got, want)
	}
}

func TestCollectCPUCountsProcessorsWithoutCgroup(t *testing.T) {
	r := fixtureRoot(t, map[string]string{"proc/cpuinfo": cpuinfo4Core})
	want := "AMD Ryzen 9 5950X 16-Core Processor (4 cores)"
	if got := CollectCPU(r); got != want {
		t.Fatalf("CollectCPU = %q, want %q", got, want)
	}
}

func TestCollectCPUEmptyWithoutCpuinfo(t *testing.T) {
	if got := CollectCPU(fixtureRoot(t, map[string]string{})); got != "" {
		t.Fatalf("CollectCPU = %q, want empty", got)
	}
}

func TestCgroupEffectiveCPUCountParsesRangesAndLists(t *testing.T) {
	cases := map[string]int{
		"0-3":       4,
		"0,2-4":     4,
		"5":         1,
		"0-1,4-5,8": 5,
	}
	for spec, want := range cases {
		r := fixtureRoot(t, map[string]string{"sys/fs/cgroup/cpuset.cpus.effective": spec + "\n"})
		got, ok := cgroupEffectiveCPUCount(r)
		if !ok || got != want {
			t.Fatalf("cgroupEffectiveCPUCount(%q) = %d,%v want %d,true", spec, got, ok, want)
		}
	}
}
