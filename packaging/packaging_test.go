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

// nonCommentLines returns all non-blank, non-comment lines from the input.
func nonCommentLines(content string) []string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}

// containsDirective checks if a directive appears as a non-comment line.
func containsDirective(lines []string, directive string) bool {
	for _, line := range lines {
		if line == directive {
			return true
		}
	}
	return false
}

// The agent runs as root on every machine in a fleet, so each of these
// directives is load-bearing. Losing one silently widens what a compromised
// agent — or a bug in it — can reach.
func TestServiceUnitKeepsItsHardening(t *testing.T) {
	unit := readFile(t, "systemd/almanaut-agent.service")
	lines := nonCommentLines(unit)
	required := []string{
		"Type=oneshot",
		"ProtectSystem=strict",
		"ProtectHome=true",
		"PrivateTmp=true",
		"NoNewPrivileges=true",
		"StateDirectory=almanaut-agent",
		"CapabilityBoundingSet=",
	}
	for _, directive := range required {
		if !containsDirective(lines, directive) {
			t.Errorf("service unit is missing %q", directive)
		}
	}
}

// StateDirectory is what makes /var/lib/almanaut-agent writable under
// ProtectSystem=strict. Without it the agent cannot persist its identity and
// would generate a new one every hour, creating a duplicate host each time.
func TestServiceUnitRunsTheInstalledBinary(t *testing.T) {
	unit := readFile(t, "systemd/almanaut-agent.service")
	lines := nonCommentLines(unit)
	if !containsDirective(lines, "ExecStart=/usr/local/bin/almanaut-agent") {
		t.Errorf("service unit does not exec the installed binary:\n%s", unit)
	}
}

// The service is timer-driven and must never be enabled directly, so an empty
// [Install] section is required: only the timer may be enabled.
func TestServiceUnitHasNoInstallSection(t *testing.T) {
	unit := readFile(t, "systemd/almanaut-agent.service")
	if strings.Contains(unit, "[Install]") {
		t.Errorf("service unit must not have an [Install] section (it is timer-driven only):\n%s", unit)
	}
}

func TestTimerSchedule(t *testing.T) {
	timer := readFile(t, "systemd/almanaut-agent.timer")
	lines := nonCommentLines(timer)
	required := []string{
		"OnBootSec=2min",
		"OnUnitActiveSec=1h",
		"RandomizedDelaySec=5min",
		"WantedBy=timers.target",
	}
	for _, directive := range required {
		if !containsDirective(lines, directive) {
			t.Errorf("timer is missing %q", directive)
		}
	}
}

// A timer whose Unit= does not match the service name silently never fires the
// agent: systemd looks for a unit of the same basename, and a typo produces a
// timer that is enabled, listed, and useless.
func TestTimerTargetsTheService(t *testing.T) {
	timer := readFile(t, "systemd/almanaut-agent.timer")
	lines := nonCommentLines(timer)
	if !containsDirective(lines, "Unit=almanaut-agent.service") {
		t.Errorf("timer is missing %q", "Unit=almanaut-agent.service")
	}
}
