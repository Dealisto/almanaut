package packaging

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Redirect the installer's destinations to a private tree and substitute only
// privilege detection and systemd activation. The real shell, install, chmod
// and configuration writes exercise installation without changing the host.
func installerFixture(t *testing.T) (string, func(...string) ([]byte, error)) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the installer targets Linux systemd hosts")
	}
	root := t.TempDir()
	script := readFile(t, "install.sh")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	for setting, directory := range map[string]string{
		"BIN_DIR=/usr/local/bin":         "bin",
		"CONFIG_DIR=/etc/almanaut-agent": "config",
		"UNIT_DIR=/etc/systemd/system":   "units",
	} {
		if strings.Count(script, setting) != 1 {
			t.Fatalf("installer destination changed: %s", setting)
		}
		key, _, _ := strings.Cut(setting, "=")
		script = strings.Replace(script, setting, key+"="+quote(filepath.Join(root, directory)), 1)
	}
	for _, name := range []string{"bin", "units", "systemd", "commands"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"install.sh":                     script,
		"almanaut-agent":                 "first binary",
		"systemd/almanaut-agent.service": "service fixture",
		"systemd/almanaut-agent.timer":   "timer fixture",
		"commands/id":                    "#!/bin/sh\nprintf '0\\n'\n",
		"commands/systemctl":             "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$ALMANAUT_INSTALL_TEST_CALLS\"\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root, func(args ...string) ([]byte, error) {
		cmd := exec.Command("/bin/sh", append([]string{"install.sh"}, args...)...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"PATH="+filepath.Join(root, "commands")+":/usr/bin:/bin",
			"ALMANAUT_SERVER_URL=", "ALMANAUT_AGENT_TOKEN=",
			"ALMANAUT_INSTALL_TEST_CALLS="+filepath.Join(root, "activation.log"))
		return cmd.CombinedOutput()
	}
}

func TestInstallerMissingConfigurationDoesNotModifyInstallation(t *testing.T) {
	for name, args := range map[string][]string{
		"neither":     nil,
		"server only": {"--server", "https://inventory.example"},
		"token only":  {"--token", "test-token"},
	} {
		t.Run(name, func(t *testing.T) {
			root, install := installerFixture(t)
			out, err := install(args...)
			if err == nil || !strings.Contains(string(out), "--server and --token are required") {
				t.Fatalf("missing configuration was not rejected: %s; %v", out, err)
			}
			for _, name := range []string{"bin/almanaut-agent", "config", "units/almanaut-agent.service", "units/almanaut-agent.timer", "activation.log"} {
				if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
					t.Errorf("failed preflight changed %s: %v", name, err)
				}
			}
		})
	}
}

func TestInstallerUpgradePreservesConfigurationWithoutArguments(t *testing.T) {
	root, install := installerFixture(t)
	if out, err := install("--server", "https://inventory.example", "--token", "test-token"); err != nil {
		t.Fatalf("fresh installation failed: %s; %v", out, err)
	}
	config := filepath.Join(root, "config/config.toml")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "almanaut-agent"), []byte("upgraded binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := install(); err != nil {
		t.Fatalf("upgrade without new credentials failed: %s; %v", out, err)
	}
	after, err := os.ReadFile(config)
	if err != nil || string(after) != string(before) {
		t.Fatalf("upgrade changed existing credentials: %v", err)
	}
	installed, err := os.ReadFile(filepath.Join(root, "bin/almanaut-agent"))
	if err != nil || string(installed) != "upgraded binary" {
		t.Fatalf("upgrade did not replace the binary: %v", err)
	}
	for name, mode := range map[string]os.FileMode{"config": 0o700, "config/config.toml": 0o600, "bin/almanaut-agent": 0o755} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("incorrect installation permissions for %s: %v", name, err)
		}
	}
	activation, err := os.ReadFile(filepath.Join(root, "activation.log"))
	want := "daemon-reload\nenable --now almanaut-agent.timer\n"
	if err != nil || string(activation) != want+want {
		t.Fatalf("installation/upgrade did not activate the timer: %q; %v", activation, err)
	}
}
