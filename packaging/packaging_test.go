// Package packaging holds the systemd units and installer shipped in the
// agent's release archive. It contains no production Go code: the tests here
// exist so CI fails when a unit file loses a security directive, which is
// otherwise invisible until something goes wrong on a real machine.
package packaging

import (
	"os"
	"os/exec"
	"os/user"
	"runtime"
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

// sectionLines extracts all lines within a named section (e.g., "[Service]").
// Returns lines from the section until the next section header or EOF.
func sectionLines(content, section string) []string {
	var lines []string
	inSection := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == section {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(trimmed, "[") && trimmed != section {
			break
		}
		if inSection {
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				lines = append(lines, trimmed)
			}
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

// stripShellComments removes lines that are comments (# prefix), excluding
// the shebang (#!). Used to ensure a shell script test cannot be satisfied by
// a comment containing the directive.
func stripShellComments(content string) string {
	var result []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "#!") {
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

// The agent runs as root on every machine in a fleet, so each of these
// directives is load-bearing. Losing one silently widens what a compromised
// agent — or a bug in it — can reach. Each must appear in the [Service]
// section, not accidentally placed under [Unit].
func TestServiceUnitKeepsItsHardening(t *testing.T) {
	unit := readFile(t, "systemd/almanaut-agent.service")
	lines := sectionLines(unit, "[Service]")
	required := []string{
		"Type=oneshot",
		"ProtectSystem=strict",
		"ProtectHome=true",
		"PrivateTmp=true",
		"NoNewPrivileges=true",
		"StateDirectory=almanaut-agent",
		"CapabilityBoundingSet=CAP_SYS_PTRACE",
	}
	for _, directive := range required {
		if !containsDirective(lines, directive) {
			t.Errorf("service unit is missing %q in [Service] section", directive)
		}
	}
}

// StateDirectory is what makes /var/lib/almanaut-agent writable under
// ProtectSystem=strict. Without it the agent cannot persist its identity and
// would generate a new one every hour, creating a duplicate host each time.
func TestServiceUnitRunsTheInstalledBinary(t *testing.T) {
	unit := readFile(t, "systemd/almanaut-agent.service")
	lines := sectionLines(unit, "[Service]")
	if !containsDirective(lines, "ExecStart=/usr/local/bin/almanaut-agent") {
		t.Errorf("service unit does not exec the installed binary in [Service] section:\n%s", unit)
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
	lines := sectionLines(timer, "[Timer]")
	required := []string{
		"OnBootSec=2min",
		"OnUnitActiveSec=1h",
		"RandomizedDelaySec=5min",
	}
	for _, directive := range required {
		if !containsDirective(lines, directive) {
			t.Errorf("timer is missing %q in [Timer] section", directive)
		}
	}
	// WantedBy is in [Install] section
	installLines := sectionLines(timer, "[Install]")
	if !containsDirective(installLines, "WantedBy=timers.target") {
		t.Errorf("timer is missing %q in [Install] section", "WantedBy=timers.target")
	}
}

// A timer whose Unit= does not match the service name silently never fires the
// agent: systemd looks for a unit of the same basename, and a typo produces a
// timer that is enabled, listed, and useless.
func TestTimerTargetsTheService(t *testing.T) {
	timer := readFile(t, "systemd/almanaut-agent.timer")
	lines := sectionLines(timer, "[Timer]")
	if !containsDirective(lines, "Unit=almanaut-agent.service") {
		t.Errorf("timer is missing %q in [Timer] section", "Unit=almanaut-agent.service")
	}
}

// The installer writes a file containing an API token, so its permissions and
// its refusal to clobber an existing config are the two properties that matter.
func TestInstallerProtectsTheConfig(t *testing.T) {
	script := readFile(t, "install.sh")
	// Use stripShellComments so a comment containing "chmod 600" does not
	// satisfy the test — the actual chmod command must be in executable code.
	scriptNoComments := stripShellComments(script)
	for _, want := range []string{
		"chmod 600", // the config holds a token
		"chmod 700", // so does its directory
		"systemctl daemon-reload",
		"systemctl enable --now almanaut-agent.timer",
	} {
		if !strings.Contains(scriptNoComments, want) {
			t.Errorf("install.sh is missing %q (not counting comments)", want)
		}
	}
	// Re-running the installer during an upgrade must not blank a token the
	// operator pasted by hand, so the write must be guarded by an existence
	// check rather than being unconditional. Check that printf (the write)
	// appears after the guard. Use stripped text to avoid spurious fails from
	// comments mentioning printf before the guard.
	guardIdx := strings.Index(scriptNoComments, "if [ -f \"$CONFIG_FILE\" ]")
	printfIdx := strings.Index(scriptNoComments, "printf")
	if guardIdx == -1 {
		t.Errorf("install.sh does not guard against overwriting an existing config (missing 'if [ -f')")
	}
	if printfIdx == -1 {
		t.Errorf("install.sh does not write the config (missing 'printf')")
	}
	if guardIdx > printfIdx {
		t.Errorf("install.sh writes the config before guarding against overwrites")
	}
}

// The installer must validate that SERVER_URL and TOKEN contain no characters
// that would corrupt the TOML config or enable shell injection.
func TestInstallerValidatesConfigValues(t *testing.T) {
	script := readFile(t, "install.sh")
	// Check that validation exists (the script must contain the validation logic).
	// These are the dangerous characters that must be rejected.
	requiredValidation := []string{
		"grep -q '\"'",   // reject double-quote
		"grep -q '\\\\'", // reject backslash
		"case",           // detect newlines using case, not grep with embedded newline
		"validate_config_value",
	}
	for _, want := range requiredValidation {
		if !strings.Contains(script, want) {
			t.Errorf("install.sh is missing validation check for %q", want)
		}
	}
	// Verify that validation is called for both SERVER_URL and TOKEN.
	if strings.Count(script, "validate_config_value") < 2 {
		t.Errorf("install.sh must call validate_config_value for both SERVER_URL and TOKEN")
	}
}

// TestInstallerBehavior runs the installer for real to verify that validation
// actually works. Running as a non-root user is safe: the script fails at the
// root check, so no installation occurs. This catches regressions like the
// broken newline check that would reject every value.
func TestInstallerBehavior(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("sh is not available on this platform; CI runs this check on Linux")
	}
	currentUser, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %v", err)
	}
	if currentUser.Uid == "0" {
		t.Skip("test must run as non-root; it validates by attempting an unprivileged install")
	}

	// Valid values should pass validation and fail on the root check, not validation.
	out, err := exec.Command("sh", "install.sh", "--server", "https://alm.lan", "--token", "alm_abc123").CombinedOutput()
	if err == nil {
		t.Fatalf("script should fail (no root): %s", out)
	}
	outStr := string(out)
	if !strings.Contains(outStr, "must run as root") {
		t.Fatalf("expected 'must run as root' error, got: %s", outStr)
	}
	if strings.Contains(outStr, "validation") || strings.Contains(outStr, "double-quote") ||
		strings.Contains(outStr, "backslash") || strings.Contains(outStr, "newline") {
		t.Fatalf("legitimate values should pass validation, got: %s", outStr)
	}

	// Token with double-quote should fail validation before the root check.
	out, err = exec.Command("sh", "install.sh", "--token", `alm_"bad`).CombinedOutput()
	if err == nil {
		t.Fatalf("token with double-quote should fail validation: %s", out)
	}
	outStr = string(out)
	if !strings.Contains(outStr, "double-quote") {
		t.Fatalf("expected validation error for double-quote, got: %s", outStr)
	}

	// Token with backslash should fail validation.
	out, err = exec.Command("sh", "install.sh", "--token", `alm_\bad`).CombinedOutput()
	if err == nil {
		t.Fatalf("token with backslash should fail validation: %s", out)
	}
	outStr = string(out)
	if !strings.Contains(outStr, "backslash") {
		t.Fatalf("expected validation error for backslash, got: %s", outStr)
	}

	// Token with newline should fail validation.
	out, err = exec.Command("sh", "install.sh", "--token", "alm_bad\nmore").CombinedOutput()
	if err == nil {
		t.Fatalf("token with newline should fail validation: %s", out)
	}
	outStr = string(out)
	if !strings.Contains(outStr, "newline") {
		t.Fatalf("expected validation error for newline, got: %s", outStr)
	}
}

// A syntax error in a shell script only surfaces when an operator runs it on a
// real machine, which is the worst possible moment. `sh -n` parses without
// executing. Linux-only: this is where the script runs and where CI runs.
//
// This is not a command-injection vector, despite the shape: all three
// arguments are compile-time constants, there is no `-c` and nothing is
// interpolated, so no shell parsing of untrusted input occurs. `-n` also means
// sh only parses and never executes the file.
func TestInstallerParses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("sh is not available on this platform; CI runs this check on Linux")
	}
	out, err := exec.Command("sh", "-n", "install.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("sh -n install.sh failed: %v\n%s", err, out)
	}
}
