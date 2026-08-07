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

// A line missing "=" is exactly the shape of an operator typo like
// `token "alm_live_abc123"` — and the raw line is the token itself. The error
// must name the line (via LoadConfig's wrapper) without ever echoing the
// value, since this error reaches stderr and from there the systemd journal.
func TestLoadConfigMalformedLineDoesNotLeakSecret(t *testing.T) {
	const secret = "alm_live_abc123"
	p := writeConfig(t, "server_url = \"https://alm.lan\"\ntoken \""+secret+"\"\n")
	_, err := LoadConfig(p, noEnv)
	if err == nil {
		t.Fatal("LoadConfig accepted a line missing '=', want error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error %q leaks the secret value", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error %q does not name the line", err)
	}
}

// A stray quote inside the value splits it: the text after the (wrong)
// closing quote is a fragment of the secret itself, so it must not be echoed
// either.
func TestLoadConfigTrailingGarbageDoesNotLeakSecretFragment(t *testing.T) {
	const secretFragment = "cd"
	p := writeConfig(t, "server_url = \"https://alm.lan\"\ntoken = \"ab\""+secretFragment+"\"\n")
	_, err := LoadConfig(p, noEnv)
	if err == nil {
		t.Fatal("LoadConfig accepted trailing garbage, want error")
	}
	if strings.Contains(err.Error(), secretFragment) {
		t.Fatalf("error %q leaks a fragment of the secret", err)
	}
	if !strings.Contains(err.Error(), "token") {
		t.Fatalf("error %q does not name the key", err)
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

// A malformed server_url must fail LoadConfig (a config error, exit 1) rather
// than survive to fail inside Send with "unsupported protocol scheme", which
// the caller treats as transient (exit 3) and retries forever.
func TestLoadConfigRejectsServerURLMissingScheme(t *testing.T) {
	p := writeConfig(t, "server_url = \"almanaut.lan\"\ntoken = \"t\"\n")
	if _, err := LoadConfig(p, noEnv); err == nil {
		t.Fatal("LoadConfig accepted a server_url with no scheme, want error")
	}
}

func TestLoadConfigRejectsServerURLNonHTTPScheme(t *testing.T) {
	p := writeConfig(t, "server_url = \"ftp://alm.lan\"\ntoken = \"t\"\n")
	if _, err := LoadConfig(p, noEnv); err == nil {
		t.Fatal("LoadConfig accepted a non-http(s) scheme, want error")
	}
}

func TestLoadConfigRejectsServerURLMissingHost(t *testing.T) {
	p := writeConfig(t, "server_url = \"https:///path\"\ntoken = \"t\"\n")
	if _, err := LoadConfig(p, noEnv); err == nil {
		t.Fatal("LoadConfig accepted a server_url with no host, want error")
	}
}

func TestLoadConfigAcceptsValidServerURLs(t *testing.T) {
	for _, u := range []string{"https://alm.lan", "http://alm.lan:8080/subpath"} {
		p := writeConfig(t, "server_url = \""+u+"\"\ntoken = \"t\"\n")
		cfg, err := LoadConfig(p, noEnv)
		if err != nil {
			t.Fatalf("LoadConfig(%q): %v", u, err)
		}
		if cfg.ServerURL != u {
			t.Fatalf("ServerURL = %q, want %q", cfg.ServerURL, u)
		}
	}
}
