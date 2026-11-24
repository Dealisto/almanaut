package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadXenOrchestra(t *testing.T) {
	for _, key := range []string{"ALMANAUT_XO_URL", "ALMANAUT_XO_TOKEN", "ALMANAUT_XO_TOKEN_FILE", "ALMANAUT_XO_INSECURE", "ALMANAUT_DISCOVERY_XO_INTERVAL"} {
		t.Setenv(key, "")
	}
	cfg := mustLoad(t)
	if cfg.XenOrchestraURL != "" || cfg.XenOrchestraToken != "" || cfg.XenOrchestraInsecure || cfg.DiscoveryXenOrchestraInterval != 0 {
		t.Fatal("Xen Orchestra must default to disabled with TLS verification")
	}
	t.Setenv("ALMANAUT_XO_URL", "https://xo.example.test")
	t.Setenv("ALMANAUT_XO_TOKEN", "test-token")
	t.Setenv("ALMANAUT_XO_INSECURE", "true")
	t.Setenv("ALMANAUT_DISCOVERY_XO_INTERVAL", "2h")
	cfg = mustLoad(t)
	if cfg.XenOrchestraURL != "https://xo.example.test" || cfg.XenOrchestraToken != "test-token" || !cfg.XenOrchestraInsecure || cfg.DiscoveryXenOrchestraInterval != 2*time.Hour {
		t.Fatal("Xen Orchestra environment overrides were not loaded")
	}
	path := filepath.Join(t.TempDir(), "xo-token")
	if err := os.WriteFile(path, []byte("file-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALMANAUT_XO_TOKEN_FILE", path)
	if got := mustLoad(t).XenOrchestraToken; got != "file-token" {
		t.Fatal("token file must take precedence and trim newline")
	}
	t.Setenv("ALMANAUT_XO_TOKEN_FILE", path+"-missing")
	if _, err := Load(); err == nil {
		t.Fatal("unreadable token file must fail configuration loading")
	}
}
