package agent

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// v1UnlimitedThreshold is where cgroup v1's "no limit" sentinel lives. v1 has
// no word for unlimited, so it stores a value near max int64 instead; anything
// at or above this is not a real limit.
const v1UnlimitedThreshold = int64(1) << 62

// CollectRAM returns the machine's memory as a human string, preferring the
// cgroup limit over /proc/meminfo.
//
// The preference is the point. Proxmox mounts lxcfs, which virtualizes
// /proc/meminfo to the container's allocation — but lxcfs may be absent, and
// then /proc/meminfo shows the host's total and every LXC on the box claims
// 64 GB. The cgroup limit is the true allocation either way.
//
// Returns "" when neither source is readable, which the server reads as
// "could not determine" and leaves the existing value alone.
func CollectRAM(r Root) string {
	if bytes, ok := cgroupMemoryLimitBytes(r); ok {
		return formatBytes(bytes)
	}
	if bytes, ok := procMemTotalBytes(r); ok {
		return formatBytes(bytes)
	}
	return ""
}

// CollectCPU returns the CPU model with its core count.
//
// Under lxcfs the model is the host's and the count the container's, which is
// correct: the processor really is the host's. The wording distinguishes the
// two cases so a reader can tell an allocation from a full machine.
func CollectCPU(r Root) string {
	model, procs := procCPUInfo(r)
	if model == "" {
		return ""
	}
	if n, ok := cgroupEffectiveCPUCount(r); ok {
		return fmt.Sprintf("%s (%d cores allocated)", model, n)
	}
	if procs > 0 {
		return fmt.Sprintf("%s (%d cores)", model, procs)
	}
	return model
}

// cgroupMemoryLimitBytes reads the cgroup v2 then v1 memory limit. The second
// return is false when there is no limit — the v2 root cgroup has no
// memory.max at all, and both versions can express "unlimited".
func cgroupMemoryLimitBytes(r Root) (int64, bool) {
	if raw := readTrimmed(r.Path(r.Sys, "fs/cgroup/memory.max")); raw != "" {
		if raw == "max" {
			return 0, false
		}
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 && n < v1UnlimitedThreshold {
			return n, true
		}
		return 0, false
	}
	raw := readTrimmed(r.Path(r.Sys, "fs/cgroup/memory/memory.limit_in_bytes"))
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 || n >= v1UnlimitedThreshold {
		return 0, false
	}
	return n, true
}

// cgroupEffectiveCPUCount counts the CPUs in the cgroup's cpuset — what
// Proxmox's `cores=N` actually sets.
func cgroupEffectiveCPUCount(r Root) (int, bool) {
	spec := readTrimmed(r.Path(r.Sys, "fs/cgroup/cpuset.cpus.effective"))
	if spec == "" {
		spec = readTrimmed(r.Path(r.Sys, "fs/cgroup/cpuset/cpuset.effective_cpus"))
	}
	if spec == "" {
		return 0, false
	}
	n := countCPUSet(spec)
	if n == 0 {
		return 0, false
	}
	return n, true
}

// countCPUSet counts the CPUs in a Linux cpu-list such as "0-3,8".
func countCPUSet(spec string) int {
	total := 0
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		start, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil {
			continue
		}
		if !isRange {
			total++
			continue
		}
		end, err := strconv.Atoi(strings.TrimSpace(hi))
		if err != nil || end < start {
			continue
		}
		total += end - start + 1
	}
	return total
}

// procMemTotalBytes reads MemTotal from /proc/meminfo, which is reported in kB.
func procMemTotalBytes(r Root) (int64, bool) {
	f, err := os.Open(r.Path(r.Proc, "meminfo"))
	if err != nil {
		return 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ":")
		if !ok || strings.TrimSpace(key) != "MemTotal" {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			return 0, false
		}
		kb, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}

// procCPUInfo returns the model name and the number of processor entries.
func procCPUInfo(r Root) (model string, processors int) {
	f, err := os.Open(r.Path(r.Proc, "cpuinfo"))
	if err != nil {
		return "", 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "processor":
			processors++
		case "model name", "Model":
			if model == "" {
				model = strings.TrimSpace(value)
			}
		}
	}
	return model, processors
}

// formatBytes renders a byte count in GB with one decimal, or TB past 1024 GB.
func formatBytes(b int64) string {
	const gb = 1024 * 1024 * 1024
	g := float64(b) / gb
	if g >= 1024 {
		return fmt.Sprintf("%.1f TB", g/1024)
	}
	return fmt.Sprintf("%.1f GB", g)
}
