package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// fixtureRoot builds a fake /proc, /sys and /etc from a path->content map and
// returns a Root pointing at it. Every collector test uses this, which is what
// lets the LXC logic be exercised in CI on an ordinary machine.
func fixtureRoot(t *testing.T, files map[string]string) Root {
	t.Helper()
	base := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(base, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return Root{
		Proc: filepath.Join(base, "proc"),
		Sys:  filepath.Join(base, "sys"),
		Etc:  filepath.Join(base, "etc"),
		Run:  filepath.Join(base, "run"),
	}
}

func TestCollectHostFactsReadsOSRelease(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"etc/os-release": "NAME=\"Debian GNU/Linux\"\nPRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nVERSION_ID=\"12\"\n",
	})
	got := CollectHostFacts(r)
	if got.OS != "Debian GNU/Linux 12 (bookworm)" {
		t.Fatalf("OS = %q", got.OS)
	}
}

// The whole report must survive a missing file. An absent os-release yields an
// empty OS, which the server reads as "unknown" and leaves alone.
func TestCollectHostFactsToleratesMissingFiles(t *testing.T) {
	r := fixtureRoot(t, map[string]string{})
	got := CollectHostFacts(r)
	if got.OS != "" {
		t.Fatalf("OS = %q, want empty when os-release is absent", got.OS)
	}
}

func TestCollectHostFactsReadsKernelAndUptime(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"proc/sys/kernel/osrelease": "6.1.0-18-amd64\n",
		"proc/uptime":               "12345.67 98765.43\n",
	})
	got := CollectHostFacts(r)
	if got.Kernel != "6.1.0-18-amd64" {
		t.Fatalf("Kernel = %q", got.Kernel)
	}
	if got.UptimeSeconds != 12345 {
		t.Fatalf("UptimeSeconds = %d, want 12345", got.UptimeSeconds)
	}
}

func TestDetectVirtKind(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "lxc via systemd container marker",
			files: map[string]string{"run/systemd/container": "lxc\n"},
			want:  "lxc",
		},
		{
			name:  "docker maps to lxc, the closest host type",
			files: map[string]string{"run/systemd/container": "docker\n"},
			want:  "lxc",
		},
		{
			name:  "container detected from pid 1 cgroup when systemd marker is absent",
			files: map[string]string{"proc/1/cgroup": "0::/docker/3f2b1c9e\n"},
			want:  "lxc",
		},
		{
			name: "container wins over the hypervisor underneath it",
			files: map[string]string{
				"run/systemd/container":         "lxc\n",
				"sys/class/dmi/id/product_name": "KVM\n",
			},
			want: "lxc",
		},
		{
			name:  "kvm guest via DMI",
			files: map[string]string{"sys/class/dmi/id/product_name": "KVM\n"},
			want:  "vm",
		},
		{
			name:  "vmware guest via DMI vendor",
			files: map[string]string{"sys/class/dmi/id/sys_vendor": "VMware, Inc.\n"},
			want:  "vm",
		},
		{
			name:  "bare metal when nothing indicates otherwise",
			files: map[string]string{"sys/class/dmi/id/sys_vendor": "ASUSTeK COMPUTER INC.\n"},
			want:  "physical",
		},
		{
			name:  "systemd unit lxcbackup.service is not a container",
			files: map[string]string{"proc/1/cgroup": "0::/system.slice/lxcbackup.service\n"},
			want:  "physical",
		},
		{
			name:  "systemd unit dockerize.service is not a container",
			files: map[string]string{"proc/1/cgroup": "0::/system.slice/dockerize.service\n"},
			want:  "physical",
		},
		{
			name:  "real docker container with scope format",
			files: map[string]string{"proc/1/cgroup": "0::/docker-abc123def456.scope\n"},
			want:  "lxc",
		},
		{
			name:  "lxc container with payload format",
			files: map[string]string{"proc/1/cgroup": "0::/lxc.payload/lxc-container-123\n"},
			want:  "lxc",
		},
		{
			name:  "kubernetes pod in kubepods slice",
			files: map[string]string{"proc/1/cgroup": "0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod123.slice\n"},
			want:  "lxc",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			for k, v := range tc.files {
				files[k] = v
			}
			if got := detectVirtKind(fixtureRoot(t, files)); got != tc.want {
				t.Fatalf("detectVirtKind = %q, want %q", got, tc.want)
			}
		})
	}
}
