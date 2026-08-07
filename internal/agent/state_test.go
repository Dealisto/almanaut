package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// uuid4Pattern also pins the version nibble (4) and the variant nibble (8-b),
// so a generator that forgot to set them would fail rather than merely
// producing 32 random hex characters.
var uuid4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestLoadOrCreateAgentIDIsStable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	first, err := LoadOrCreateAgentID(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if !uuid4Pattern.MatchString(first) {
		t.Fatalf("id %q is not a UUIDv4", first)
	}
	second, err := LoadOrCreateAgentID(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if second != first {
		t.Fatalf("id changed across calls: %q then %q", first, second)
	}
}

func TestLoadOrCreateAgentIDWritesRestrictivePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if _, err := LoadOrCreateAgentID(dir); err != nil {
		t.Fatalf("LoadOrCreateAgentID: %v", err)
	}
	// Windows does not model Unix permission bits; the assertion is only
	// meaningful on the platform the agent actually runs on.
	if os.PathSeparator == '/' {
		fi, err := os.Stat(filepath.Join(dir, "agent-id"))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("agent-id mode = %o, want 600", perm)
		}
	}
}

func TestResetAgentIDReplacesTheID(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	before, err := LoadOrCreateAgentID(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateAgentID: %v", err)
	}
	after, err := ResetAgentID(dir)
	if err != nil {
		t.Fatalf("ResetAgentID: %v", err)
	}
	if after == before {
		t.Fatal("ResetAgentID returned the same id")
	}
	again, err := LoadOrCreateAgentID(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if again != after {
		t.Fatalf("reload = %q, want the reset id %q", again, after)
	}
}

// A file left behind with stray whitespace (hand-edited, or written by an
// older version) must still load rather than poisoning every report with an
// id the server cannot match.
func TestLoadOrCreateAgentIDTrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	want := "3f2b1c9e-0000-4000-8000-000000000001"
	if err := os.WriteFile(filepath.Join(dir, "agent-id"), []byte("  "+want+"\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := LoadOrCreateAgentID(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateAgentID: %v", err)
	}
	if got != want {
		t.Fatalf("id = %q, want %q", got, want)
	}
}

// An empty or whitespace-only file is corruption, not an id: regenerate
// rather than reporting "" and being rejected by the server every hour.
func TestLoadOrCreateAgentIDReplacesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent-id"), []byte("\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := LoadOrCreateAgentID(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateAgentID: %v", err)
	}
	if !uuid4Pattern.MatchString(got) {
		t.Fatalf("id %q is not a UUIDv4", got)
	}
}
