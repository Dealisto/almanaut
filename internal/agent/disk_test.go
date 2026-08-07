package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestCollectDisksListsBlockDevices(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		// 512-byte sectors: 1953525168 sectors ≈ 931.5 GB
		"sys/block/nvme0n1/size":         "1953525168\n",
		"sys/block/nvme0n1/device/model": "Samsung SSD 990 PRO 1TB\n",
		"sys/block/sda/size":             "7814037168\n",
		"sys/block/sda/device/model":     "WDC WD40EFRX\n",
		// Virtual devices must be skipped.
		"sys/block/loop0/size": "0\n",
		"sys/block/dm-0/size":  "1953525168\n",
	})
	summary, disks := CollectDisks(r, false, nil)
	if len(disks) != 2 {
		t.Fatalf("disks = %+v, want exactly the two real devices", disks)
	}
	if disks[0].Device != "nvme0n1" || disks[1].Device != "sda" {
		t.Fatalf("devices = %q,%q want nvme0n1,sda (sorted)", disks[0].Device, disks[1].Device)
	}
	if disks[0].Model != "Samsung SSD 990 PRO 1TB" {
		t.Fatalf("model = %q", disks[0].Model)
	}
	if disks[0].SizeBytes != 1953525168*512 {
		t.Fatalf("SizeBytes = %d", disks[0].SizeBytes)
	}
	if summary == "" {
		t.Fatal("summary is empty")
	}
	for _, unwanted := range []string{"loop0", "dm-0"} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("summary %q includes virtual device %q", summary, unwanted)
		}
	}
}

// Inside a container the block devices belong to the host, so reporting them
// would describe the wrong machine. The rootfs is what the container has.
func TestCollectDisksInContainerReportsRootFilesystem(t *testing.T) {
	r := fixtureRoot(t, map[string]string{"sys/block/nvme0n1/size": "1953525168\n"})
	usage := func(path string) (int64, int64, error) {
		if path != "/" {
			t.Fatalf("usage called with %q, want /", path)
		}
		return 34359738368, 19327352832, nil // 32 GB total, 18 GB used
	}
	summary, disks := CollectDisks(r, true, usage)
	if len(disks) != 1 || disks[0].Mount != "/" {
		t.Fatalf("disks = %+v, want a single rootfs entry", disks)
	}
	if disks[0].SizeBytes != 34359738368 || disks[0].UsedBytes != 19327352832 {
		t.Fatalf("sizes = %+v", disks[0])
	}
	if summary != "rootfs 32.0 GB (18.0 GB used)" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestCollectDisksEmptyWhenUsageFails(t *testing.T) {
	r := fixtureRoot(t, map[string]string{})
	usage := func(string) (int64, int64, error) { return 0, 0, errors.New("nope") }
	summary, disks := CollectDisks(r, true, usage)
	if summary != "" || len(disks) != 0 {
		t.Fatalf("summary=%q disks=%+v, want empty so the server keeps the existing value", summary, disks)
	}
}

func TestCollectDisksEmptyWhenNoBlockDir(t *testing.T) {
	summary, disks := CollectDisks(fixtureRoot(t, map[string]string{}), false, nil)
	if summary != "" || len(disks) != 0 {
		t.Fatalf("summary=%q disks=%+v, want empty", summary, disks)
	}
}
