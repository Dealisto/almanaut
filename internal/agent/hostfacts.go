package agent

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// HostFacts are OS, kernel version, virtualization kind, and system uptime.
type HostFacts struct {
	OS            string
	Kernel        string
	VirtKind      string // physical | vm | lxc
	UptimeSeconds int64
}

// CollectHostFacts gathers OS, kernel, uptime and virtualization kind.
//
// It deliberately returns no error: each field is independent, and an empty
// field is the wire signal for "could not determine". Failing the whole report
// because one file was unreadable would lose the fields that did work.
func CollectHostFacts(r Root) HostFacts {
	return HostFacts{
		OS:            prettyOSName(r),
		Kernel:        readTrimmed(r.Path(r.Proc, "sys/kernel/osrelease")),
		VirtKind:      detectVirtKind(r),
		UptimeSeconds: readUptimeSeconds(r),
	}
}

// prettyOSName returns os-release's PRETTY_NAME, falling back to NAME.
func prettyOSName(r Root) string {
	f, err := os.Open(r.Path(r.Etc, "os-release"))
	if err != nil {
		return ""
	}
	defer f.Close()
	var name string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.TrimSpace(key) {
		case "PRETTY_NAME":
			if value != "" {
				return value
			}
		case "NAME":
			name = value
		}
	}
	return name
}

// readUptimeSeconds reads the whole-seconds part of /proc/uptime.
func readUptimeSeconds(r Root) int64 {
	raw := readTrimmed(r.Path(r.Proc, "uptime"))
	first, _, _ := strings.Cut(raw, " ")
	whole, _, _ := strings.Cut(first, ".")
	n, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// containerRuntimes are the values systemd's container marker can hold. They
// all map to "lxc": domain.HostTypes has no separate container value, and lxc
// is the closest available truth for "a container on some host".
var containerRuntimes = map[string]bool{
	"lxc": true, "lxc-libvirt": true, "systemd-nspawn": true,
	"docker": true, "podman": true, "containerd": true, "rkt": true,
}

// vmVendorMarkers are substrings that identify a hypervisor in DMI strings.
var vmVendorMarkers = []string{
	"kvm", "qemu", "vmware", "virtualbox", "innotek", "xen",
	"microsoft corporation", "bochs", "parallels", "bhyve", "amazon ec2",
}

// detectVirtKind returns physical, vm or lxc.
//
// Container detection comes first and wins: a container running on a VM is a
// container as far as the inventory is concerned, and its DMI would otherwise
// report the hypervisor underneath it.
//
// The pid-1 cgroup is the fallback because a container without systemd — a
// plain `docker run` — has no /run/systemd/container marker at all, but its
// init process is always in a namespaced cgroup path naming the runtime.
func detectVirtKind(r Root) string {
	if marker := readTrimmed(r.Path(r.Run, "systemd/container")); marker != "" {
		return "lxc"
	}
	initCgroup := strings.ToLower(readTrimmed(r.Path(r.Proc, "1/cgroup")))

	// Check for container runtimes by matching path segments, not raw substrings.
	// This avoids false positives: systemd unit names like "lxcbackup.service"
	// would match "/lxc" in "/system.slice/lxcbackup.service" with a substring check.
	// We match either exact segments ("docker") or scope prefixes ("docker-<id>.scope").
	segments := strings.Split(initCgroup, "/")
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		for runtime := range containerRuntimes {
			if segment == runtime || strings.HasPrefix(segment, runtime+"-") {
				return "lxc"
			}
		}
		// Kubernetes pods are always containers, detected as whole-segment containment.
		if strings.Contains(segment, "kubepods") {
			return "lxc"
		}
	}

	for _, file := range []string{"class/dmi/id/product_name", "class/dmi/id/sys_vendor"} {
		v := strings.ToLower(readTrimmed(r.Path(r.Sys, file)))
		for _, marker := range vmVendorMarkers {
			if strings.Contains(v, marker) {
				return "vm"
			}
		}
	}
	return "physical"
}
