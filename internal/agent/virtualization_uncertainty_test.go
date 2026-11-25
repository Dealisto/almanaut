package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectUnreadableContainerSignalAvoidsHostDisks(t *testing.T) {
	for _, failure := range []string{"missing", "unreadable"} {
		for _, vendor := range []string{"ASUSTeK COMPUTER INC.", "QEMU"} {
			t.Run(failure+"/"+vendor, func(t *testing.T) {
				r := fixtureRoot(t, map[string]string{
					"proc/1/cgroup":               "0::/init.scope\n",
					"sys/class/dmi/id/sys_vendor": vendor,
					"sys/block/sda/size":          "2147483648\n",
				})
				if failure == "unreadable" {
					// A directory reliably makes ReadFile fail, including when the
					// test runs as root and file-mode restrictions would not apply.
					if err := os.Mkdir(filepath.Join(r.Proc, "1/environ"), 0o700); err != nil {
						t.Fatal(err)
					}
				}
				rep := Collect(CollectOptions{
					Root: r, AgentID: "test", Hostname: "container",
					Usage: func(string) (int64, int64, error) { return 8 << 30, 1 << 30, nil },
				})
				if rep.VirtKind != "" {
					t.Fatalf("unreadable signal inferred virtualization %q", rep.VirtKind)
				}
				if rep.Disk != "rootfs 8.0 GB (1.0 GB used)" || len(rep.Disks) != 1 || rep.Disks[0].Device != "rootfs" {
					t.Fatalf("inconclusive detection reported host disks: %+v", rep)
				}
			})
		}
	}
}

func TestPositiveContainerSignalDoesNotRequireReadableEnviron(t *testing.T) {
	for _, tc := range []struct{ path, value string }{
		{"run/systemd/container", "lxc"},
		{"proc/1/cgroup", "0::/docker/1234"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := fixtureRoot(t, map[string]string{tc.path: tc.value})
			if got := detectVirtKind(r); got != "lxc" {
				t.Fatalf("positive container signal lost: %q", got)
			}
		})
	}
}
