package agent

import (
	"math"
	"strconv"
	"testing"
)

func TestCollectDisksDoesNotPublishPartialOrOverflowedInventory(t *testing.T) {
	for _, size := range []string{"", "not-a-size", "-1", strconv.FormatInt(math.MaxInt64/sectorSize+1, 10)} {
		t.Run(size, func(t *testing.T) {
			r := fixtureRoot(t, map[string]string{
				"sys/block/sda/size": "2097152",
				"sys/block/sdb/size": size,
			})
			summary, disks := CollectDisks(r, "physical", nil)
			if summary != "" || len(disks) != 0 {
				t.Fatalf("incomplete storage collection must preserve existing inventory: summary=%q disks=%+v", summary, disks)
			}
		})
	}
	t.Run("unreadable size", func(t *testing.T) {
		r := fixtureRoot(t, map[string]string{
			"sys/block/sda/size":         "2097152",
			"sys/block/sdb/device/model": "Temporarily unavailable disk",
		})
		summary, disks := CollectDisks(r, "physical", nil)
		if summary != "" || len(disks) != 0 {
			t.Fatalf("missing device size must preserve existing inventory: summary=%q disks=%+v", summary, disks)
		}
	})
}

func TestCollectDisksAllowsEmptyDevices(t *testing.T) {
	r := fixtureRoot(t, map[string]string{"sys/block/sda/size": "2097152", "sys/block/sdb/size": "0"})
	summary, disks := CollectDisks(r, "physical", nil)
	if summary == "" || len(disks) != 1 || disks[0].Device != "sda" {
		t.Fatalf("a readable empty device must not invalidate storage collection: %q %+v", summary, disks)
	}
}

func TestCollectDisksRejectsInconsistentFilesystemUsage(t *testing.T) {
	for _, used := range []int64{-1, 101} {
		summary, disks := CollectDisks(Root{}, "lxc", func(string) (int64, int64, error) { return 100, used, nil })
		if summary != "" || len(disks) != 0 {
			t.Errorf("invalid used bytes %d became inventory: %q %+v", used, summary, disks)
		}
	}
}
