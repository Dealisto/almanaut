package agent

import (
	"os"
	"path/filepath"
	"strings"
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
		Proc:   filepath.Join(base, "proc"),
		Sys:    filepath.Join(base, "sys"),
		Etc:    filepath.Join(base, "etc"),
		Run:    filepath.Join(base, "run"),
		UsrLib: filepath.Join(base, "usr/lib"),
	}
}

func TestOSReleaseSyntaxAndPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"vendor fallback", map[string]string{"usr/lib/os-release": `PRETTY_NAME="Vendor Linux"`}, "Vendor Linux"},
		{"etc overrides vendor", map[string]string{"etc/os-release": `NAME="Local Linux"`, "usr/lib/os-release": `PRETTY_NAME="Vendor Linux"`}, "Local Linux"},
		{"empty etc does not merge vendor", map[string]string{"etc/os-release": "", "usr/lib/os-release": `NAME="Vendor Linux"`}, ""},
		{"single quotes", map[string]string{"etc/os-release": `PRETTY_NAME='Example Linux'`}, "Example Linux"},
		{"shell escapes", map[string]string{"etc/os-release": "PRETTY_NAME=\"Example \\\"Edition\\\" \\$HOME \\`literal\\` \\\\path\\q\""}, "Example \"Edition\" $HOME `literal` \\path\\q"},
		{"last assignment wins", map[string]string{"etc/os-release": "PRETTY_NAME=Old\nPRETTY_NAME=New\n"}, "New"},
		{"malformed quote", map[string]string{"etc/os-release": "PRETTY_NAME=\"Incomplete\n"}, ""},
		{"concatenation is unsupported", map[string]string{"etc/os-release": `PRETTY_NAME="One""Two"`}, ""},
		{"read error after pretty name", map[string]string{"etc/os-release": "PRETTY_NAME=Old\n" + strings.Repeat("x", 100000)}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := prettyOSName(fixtureRoot(t, tc.files)); got != tc.want {
				t.Fatalf("OS = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMicrosoftHardwareIsNotInferredToBeVirtual(t *testing.T) {
	for _, tc := range []struct{ product, version, want string }{
		{"Surface Pro 7", "", "physical"},
		{"Surface Laptop", "", "physical"},
		{"Virtual Machine", "", "vm"},
		{"", "Hyper-V UEFI Release v4.1", "vm"},
	} {
		t.Run(tc.product+tc.version, func(t *testing.T) {
			r := fixtureRoot(t, map[string]string{
				"proc/1/environ":                   "",
				"sys/class/dmi/id/sys_vendor":      "Microsoft Corporation",
				"sys/class/dmi/id/product_name":    tc.product,
				"sys/class/dmi/id/product_version": tc.version,
			})
			if got := detectVirtKind(r); got != tc.want {
				t.Fatalf("virtualization = %q, want %q", got, tc.want)
			}
		})
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

// A scanner error partway through os-release must discard whatever was
// already accumulated rather than return a partial read as if it were
// complete: NAME was captured before the oversized line breaks the scan, but
// the function must still report "could not determine", not that fragment.
func TestCollectHostFactsEmptyOnTruncatedOSRelease(t *testing.T) {
	longLine := strings.Repeat("x", 100000)
	r := fixtureRoot(t, map[string]string{
		"etc/os-release": "NAME=\"Debian\"\n" + longLine + "\nPRETTY_NAME=\"Debian GNU/Linux 12\"\n",
	})
	got := CollectHostFacts(r)
	if got.OS != "" {
		t.Fatalf("OS = %q, want empty when the scan hits an error before completing", got.OS)
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
		// Systemd container marker takes precedence
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
			name: "container wins over the hypervisor underneath it",
			files: map[string]string{
				"run/systemd/container":         "lxc\n",
				"sys/class/dmi/id/product_name": "KVM\n",
			},
			want: "lxc",
		},

		// Physical: false positive prevention
		{
			name:  "systemd unit docker.service is not a container",
			files: map[string]string{"proc/1/cgroup": "0::/system.slice/docker.service\n", "proc/1/environ": ""},
			want:  "physical",
		},
		{
			name:  "systemd unit lxcbackup.service is not a container",
			files: map[string]string{"proc/1/cgroup": "0::/system.slice/lxcbackup.service\n", "proc/1/environ": ""},
			want:  "physical",
		},
		{
			name:  "systemd unit dockerize.service is not a container",
			files: map[string]string{"proc/1/cgroup": "0::/system.slice/dockerize.service\n", "proc/1/environ": ""},
			want:  "physical",
		},
		{
			name:  "empty cgroup path is not a container",
			files: map[string]string{"proc/1/cgroup": "0::/\n", "proc/1/environ": ""},
			want:  "physical",
		},
		{
			name:  "systemd user slice is not a container",
			files: map[string]string{"proc/1/cgroup": "11:name=systemd:/user.slice/user-1000.slice\n", "proc/1/environ": ""},
			want:  "physical",
		},

		// Real containers via cgroup
		{
			name:  "docker container direct path",
			files: map[string]string{"proc/1/cgroup": "0::/docker/3f2b1c9e\n"},
			want:  "lxc",
		},
		{
			name:  "docker container with systemd scope",
			files: map[string]string{"proc/1/cgroup": "0::/system.slice/docker-3f2b1c9e.scope\n"},
			want:  "lxc",
		},
		{
			name:  "proxmox lxc container payload format",
			files: map[string]string{"proc/1/cgroup": "0::/lxc.payload.101/system.slice/init.scope\n"},
			want:  "lxc",
		},
		{
			name:  "proxmox lxc monitor container",
			files: map[string]string{"proc/1/cgroup": "0::/lxc.monitor.101\n"},
			want:  "lxc",
		},
		{
			name:  "kubernetes pod in kubepods",
			files: map[string]string{"proc/1/cgroup": "0::/kubepods/besteffort/pod123/abc\n"},
			want:  "lxc",
		},
		{
			name: "lxc via proc/1/environ when cgroup names no runtime (non-systemd init)",
			files: map[string]string{
				"proc/1/cgroup":  "0::/init.scope\n",
				"proc/1/environ": "PATH=/usr/bin\x00container=lxc\x00HOME=/root\x00",
			},
			want: "lxc",
		},
		{
			name: "podman via proc/1/environ under a private cgroup namespace",
			files: map[string]string{
				"proc/1/cgroup":  "0::/\n",
				"proc/1/environ": "PATH=/usr/bin\x00container=podman\x00",
			},
			want: "lxc",
		},
		{
			name: "systemd-nspawn via proc/1/environ under a private cgroup namespace",
			files: map[string]string{
				"proc/1/cgroup":  "0::/\n",
				"proc/1/environ": "container=systemd-nspawn\x00PATH=/usr/bin\x00",
			},
			want: "lxc",
		},
		{
			name: "unrecognized runtime in proc/1/environ is still treated as a container",
			files: map[string]string{
				"proc/1/cgroup":  "0::/\n",
				"proc/1/environ": "container=incus\x00PATH=/usr/bin\x00",
			},
			want: "lxc",
		},
		{
			name: "container substring in an unrelated variable name is not a marker",
			files: map[string]string{
				"proc/1/cgroup":  "0::/\n",
				"proc/1/environ": "MY_container=lxc\x00PATH=/usr/bin\x00",
			},
			want: "physical",
		},

		// VM detection
		{
			name:  "kvm guest via DMI",
			files: map[string]string{"sys/class/dmi/id/product_name": "KVM\n", "proc/1/environ": ""},
			want:  "vm",
		},
		{
			name:  "vmware guest via DMI vendor",
			files: map[string]string{"sys/class/dmi/id/sys_vendor": "VMware, Inc.\n", "proc/1/environ": ""},
			want:  "vm",
		},

		// Physical/bare metal
		{
			name:  "bare metal when nothing indicates otherwise",
			files: map[string]string{"sys/class/dmi/id/sys_vendor": "ASUSTeK COMPUTER INC.\n", "proc/1/environ": ""},
			want:  "physical",
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
