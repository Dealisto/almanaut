package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

func noEnv(string) string { return "" }

func TestLoadConfigReadsBothKeys(t *testing.T) {
	p := writeConfig(t, "# almanaut agent\nserver_url = \"https://alm.lan\"\ntoken = \"alm_abc\"\n")
	cfg, err := LoadConfig(p, noEnv)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ServerURL != "https://alm.lan" || cfg.Token != "alm_abc" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

// The token is the one value an operator may want to inject from a secret
// store rather than leave on disk, so the environment wins over the file.
func TestLoadConfigEnvOverridesToken(t *testing.T) {
	p := writeConfig(t, "server_url = \"https://alm.lan\"\ntoken = \"from-file\"\n")
	cfg, err := LoadConfig(p, func(k string) string {
		if k == "ALMANAUT_AGENT_TOKEN" {
			return "from-env"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Token != "from-env" {
		t.Fatalf("Token = %q, want from-env", cfg.Token)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "nope.toml"), noEnv); err == nil {
		t.Fatal("LoadConfig on a missing file = nil, want error")
	}
}

func TestLoadConfigNamesTheMissingKey(t *testing.T) {
	p := writeConfig(t, "server_url = \"https://alm.lan\"\n")
	_, err := LoadConfig(p, noEnv)
	if err == nil {
		t.Fatal("LoadConfig without a token = nil, want error")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Fatalf("error %q does not name the missing key", err)
	}
}

// Rejecting unparseable lines is deliberate: silently ignoring a real TOML
// table would leave an operator believing a setting took effect.
func TestLoadConfigRejectsUnsupportedSyntax(t *testing.T) {
	for _, body := range []string{
		"[server]\nurl = \"x\"\n",
		"server_url = https://alm.lan\n",
		"nonsense\n",
	} {
		p := writeConfig(t, body+"token = \"t\"\n")
		if _, err := LoadConfig(p, noEnv); err == nil {
			t.Fatalf("LoadConfig accepted unsupported syntax: %q", body)
		}
	}
}

func TestLoadConfigIgnoresCommentsAndBlankLines(t *testing.T) {
	p := writeConfig(t, "\n# a comment\n\nserver_url = \"https://alm.lan\"  # trailing note\ntoken = \"t\"\n")
	cfg, err := LoadConfig(p, noEnv)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ServerURL != "https://alm.lan" {
		t.Fatalf("ServerURL = %q", cfg.ServerURL)
	}
}

// Trailing comments are allowed; trailing garbage is not. This test pair
// confirms the distinction: both have trailing text, but one parses and one fails.
func TestLoadConfigRejectsTrailingGarbage(t *testing.T) {
	p := writeConfig(t, "server_url = \"https://alm.lan\" typo=oops\ntoken = \"t\"\n")
	_, err := LoadConfig(p, noEnv)
	if err == nil {
		t.Fatal("LoadConfig accepted trailing garbage, want error")
	}
	if !strings.Contains(err.Error(), "unexpected text") {
		t.Fatalf("error %q does not describe the issue", err)
	}
}

func TestLoadConfigRejectsBackslashInValue(t *testing.T) {
	p := writeConfig(t, "server_url = \"https://alm.lan\"\ntoken = \"abc\\def\"\n")
	_, err := LoadConfig(p, noEnv)
	if err == nil {
		t.Fatal("LoadConfig accepted backslash in value, want error")
	}
	if !strings.Contains(err.Error(), "escape sequence") {
		t.Fatalf("error %q does not name the problem", err)
	}
}

func TestLoadConfigRejectsMultiLineStringOpener(t *testing.T) {
	p := writeConfig(t, "server_url = \"\"\"\ntoken = \"t\"\n")
	_, err := LoadConfig(p, noEnv)
	if err == nil {
		t.Fatal("LoadConfig accepted multi-line string, want error")
	}
	if !strings.Contains(err.Error(), "multi-line") {
		t.Fatalf("error %q does not name the problem", err)
	}
}

func TestLoadConfigRejectsDuplicateKey(t *testing.T) {
	p := writeConfig(t, "server_url = \"https://alm.lan\"\ntoken = \"t1\"\ntoken = \"t2\"\n")
	_, err := LoadConfig(p, noEnv)
	if err == nil {
		t.Fatal("LoadConfig accepted duplicate key, want error")
	}
	if !strings.Contains(err.Error(), "duplicate key") || !strings.Contains(err.Error(), "token") {
		t.Fatalf("error %q does not name the duplicate key", err)
	}
	if !strings.Contains(err.Error(), "line") {
		t.Fatalf("error %q does not mention lines", err)
	}
}
