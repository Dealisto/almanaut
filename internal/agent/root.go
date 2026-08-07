package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// Root is where the collectors read the system from. Production passes the
// real /proc, /sys, /etc and /run; tests pass fixture trees.
//
// Without this indirection the LXC and cgroup logic would only be testable on
// a real container, which in practice means not tested at all — so every
// collector takes a Root and nothing reads an absolute system path directly.
// That includes /run: hardcoding the container marker's path would make the
// suite consult the real machine, passing on a laptop and reporting "lxc" the
// day CI moves into a container.
type Root struct {
	Proc string
	Sys  string
	Etc  string
	Run  string
}

// SystemRoot is the real machine.
func SystemRoot() Root {
	return Root{Proc: "/proc", Sys: "/sys", Etc: "/etc", Run: "/run"}
}

// Path joins a relative path onto one of the roots.
func (r Root) Path(base, rel string) string {
	return filepath.Join(base, filepath.FromSlash(rel))
}

// readTrimmed returns the trimmed contents of path, or "" if it cannot be
// read. Every collector treats "" as "could not determine", which the server
// turns into "leave the existing value alone" — so an unreadable file costs
// one field, never the whole report.
func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
