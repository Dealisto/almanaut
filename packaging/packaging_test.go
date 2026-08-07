// Package packaging holds the systemd units and installer shipped in the
// agent's release archive. It contains no production Go code: the tests here
// exist so CI fails when a unit file loses a security directive, which is
// otherwise invisible until something goes wrong on a real machine.
package packaging

import (
	"os"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// The agent runs as root on every machine in a fleet, so each of these
// directives is load-bearing. Losing one silently widens what a compromised
// agent — or a bug in it — can reach.
func TestServiceUnitKeepsItsHardening(t *testing.T) {
	unit := readFile(t, "systemd/almanaut-agent.service")
	required := []string{
		"Type=oneshot",
		"ProtectSystem=strict",
		"ProtectHome=true",
		"PrivateTmp=true",
		"NoNewPrivileges=true",
		"StateDirectory=almanaut-agent",
	}
	for _, directive := range required {
		if !strings.Contains(unit, directive) {
			t.Errorf("service unit is missing %q", directive)
		}
	}
}

// StateDirectory is what makes /var/lib/almanaut-agent writable under
// ProtectSystem=strict. Without it the agent cannot persist its identity and
// would generate a new one every hour, creating a duplicate host each time.
func TestServiceUnitRunsTheInstalledBinary(t *testing.T) {
	unit := readFile(t, "systemd/almanaut-agent.service")
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/almanaut-agent") {
		t.Errorf("service unit does not exec the installed binary:\n%s", unit)
	}
}

func TestTimerSchedule(t *testing.T) {
	timer := readFile(t, "systemd/almanaut-agent.timer")
	required := []string{
		"OnBootSec=2min",
		"OnUnitActiveSec=1h",
		"RandomizedDelaySec=5min",
		"WantedBy=timers.target",
	}
	for _, directive := range required {
		if !strings.Contains(timer, directive) {
			t.Errorf("timer is missing %q", directive)
		}
	}
}

// A timer whose Unit= does not match the service name silently never fires the
// agent: systemd looks for a unit of the same basename, and a typo produces a
// timer that is enabled, listed, and useless.
func TestTimerTargetsTheService(t *testing.T) {
	timer := readFile(t, "systemd/almanaut-agent.timer")
	if strings.Contains(timer, "Unit=") && !strings.Contains(timer, "Unit=almanaut-agent.service") {
		t.Errorf("timer has a Unit= that is not almanaut-agent.service:\n%s", timer)
	}
}
