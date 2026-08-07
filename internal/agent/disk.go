package agent

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/agentapi"
)

// SystemDiskUsage lives in statfs_unix.go / statfs_other.go — see Step 4.

// sectorSize is the unit of <sys>/block/<dev>/size, which the kernel always
// reports in 512-byte sectors regardless of the device's physical block size.
const sectorSize = 512

// virtualDevicePrefixes are block devices nobody inventories as hardware.
var virtualDevicePrefixes = []string{"loop", "ram", "zram", "dm-", "sr", "md"}

// CollectDisks describes storage, differently depending on where it runs.
//
// Enumerating block devices inside a container would describe the host's
// hardware, not the container — a container only meaningfully has its rootfs.
// usage is injected so that path is testable without a real filesystem; it may
// be nil when virt is not "physical" or "vm".
//
// virt gates which path runs, and it must be a positive determination rather
// than the mere absence of container evidence: enumerating <sys>/block only
// happens for the two virt kinds detection can affirmatively name as not a
// container ("physical", "vm"). Anything else — "lxc", an unrecognized value,
// or an empty string from detection that came back inconclusive — falls back
// to reporting this process's own rootfs. That fallback can under-report (an
// empty summary, or the container's own small view of "/") but it can never
// fabricate the host's hardware for a machine that does not own it.
func CollectDisks(r Root, virt string, usage func(string) (int64, int64, error)) (string, []agentapi.Disk) {
	if virt == "physical" || virt == "vm" {
		return blockDevices(r)
	}
	return rootFilesystem(usage)
}

func rootFilesystem(usage func(string) (int64, int64, error)) (string, []agentapi.Disk) {
	if usage == nil {
		return "", nil
	}
	total, used, err := usage("/")
	if err != nil || total <= 0 {
		return "", nil
	}
	d := agentapi.Disk{
		Device:    "rootfs",
		Mount:     "/",
		SizeBytes: total,
		UsedBytes: used,
	}
	return fmt.Sprintf("rootfs %s (%s used)", formatBytes(total), formatBytes(used)), []agentapi.Disk{d}
}

func blockDevices(r Root) (string, []agentapi.Disk) {
	entries, err := os.ReadDir(r.Path(r.Sys, "block"))
	if err != nil {
		return "", nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !isVirtualDevice(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	disks := make([]agentapi.Disk, 0, len(names))
	parts := make([]string, 0, len(names))
	for _, name := range names {
		sectors, err := strconv.ParseInt(readTrimmed(r.Path(r.Sys, "block/"+name+"/size")), 10, 64)
		if err != nil || sectors <= 0 {
			continue
		}
		size := sectors * sectorSize
		disks = append(disks, agentapi.Disk{
			Device:    name,
			Model:     readTrimmed(r.Path(r.Sys, "block/"+name+"/device/model")),
			SizeBytes: size,
		})
		parts = append(parts, fmt.Sprintf("%s %s", name, formatBytes(size)))
	}
	if len(disks) == 0 {
		return "", nil
	}
	return strings.Join(parts, ", "), disks
}

func isVirtualDevice(name string) bool {
	for _, p := range virtualDevicePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
