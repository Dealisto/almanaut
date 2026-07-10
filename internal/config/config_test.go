package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mustLoad calls Load and fails the test if it errors. Most tests exercise
// non-secret config where Load never errors.
func mustLoad(t *testing.T) Config {
	t.Helper()
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("ALMANAUT_ADDR", "")
	t.Setenv("ALMANAUT_DATA_DIR", "")
	c := mustLoad(t)
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want \":8080\"", c.Addr)
	}
	if c.DataDir != "./data" {
		t.Errorf("DataDir = %q, want \"./data\"", c.DataDir)
	}
	t.Setenv("ALMANAUT_DOCKER_SOCKET", "")
	if c := mustLoad(t); c.DockerSocket != "/var/run/docker.sock" {
		t.Errorf("DockerSocket = %q, want \"/var/run/docker.sock\"", c.DockerSocket)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("ALMANAUT_ADDR", ":9000")
	t.Setenv("ALMANAUT_DATA_DIR", "/var/almanaut")
	c := mustLoad(t)
	if c.Addr != ":9000" {
		t.Errorf("Addr = %q, want \":9000\"", c.Addr)
	}
	if c.DataDir != "/var/almanaut" {
		t.Errorf("DataDir = %q, want \"/var/almanaut\"", c.DataDir)
	}
	t.Setenv("ALMANAUT_DOCKER_SOCKET", "/tmp/docker.sock")
	if c := mustLoad(t); c.DockerSocket != "/tmp/docker.sock" {
		t.Errorf("DockerSocket = %q, want \"/tmp/docker.sock\"", c.DockerSocket)
	}
}

func TestLoadNetworkScanDefaults(t *testing.T) {
	t.Setenv("ALMANAUT_ENABLE_NETWORK_SCAN", "")
	t.Setenv("ALMANAUT_SCAN_SUBNET", "")
	c := mustLoad(t)
	if c.NetworkScanEnabled {
		t.Error("NetworkScanEnabled should default to false")
	}
	if c.ScanSubnet != "" {
		t.Errorf("ScanSubnet = %q, want empty", c.ScanSubnet)
	}
}

func TestLoadNetworkScanFromEnv(t *testing.T) {
	t.Setenv("ALMANAUT_ENABLE_NETWORK_SCAN", "true")
	t.Setenv("ALMANAUT_SCAN_SUBNET", "10.0.0.0/24")
	c := mustLoad(t)
	if !c.NetworkScanEnabled {
		t.Error("NetworkScanEnabled should be true for \"true\"")
	}
	if c.ScanSubnet != "10.0.0.0/24" {
		t.Errorf("ScanSubnet = %q, want 10.0.0.0/24", c.ScanSubnet)
	}
}

func TestGetenvBoolVariants(t *testing.T) {
	cases := map[string]bool{"1": true, "true": true, "YES": true, "0": false, "false": false, "nope": false}
	for v, want := range cases {
		t.Setenv("ALMANAUT_ENABLE_NETWORK_SCAN", v)
		if got := mustLoad(t).NetworkScanEnabled; got != want {
			t.Errorf("value %q -> %v, want %v", v, got, want)
		}
	}
}

func TestLoadProxmox(t *testing.T) {
	t.Setenv("ALMANAUT_PROXMOX_URL", "https://pve.lan:8006")
	t.Setenv("ALMANAUT_PROXMOX_TOKEN", "root@pam!tok=secret")
	t.Setenv("ALMANAUT_PROXMOX_INSECURE", "true")
	cfg := mustLoad(t)
	if cfg.ProxmoxURL != "https://pve.lan:8006" {
		t.Errorf("ProxmoxURL = %q", cfg.ProxmoxURL)
	}
	if cfg.ProxmoxToken != "root@pam!tok=secret" {
		t.Errorf("ProxmoxToken = %q", cfg.ProxmoxToken)
	}
	if !cfg.ProxmoxInsecure {
		t.Error("ProxmoxInsecure = false, want true")
	}
}

func TestLoadSecureCookies(t *testing.T) {
	t.Setenv("ALMANAUT_SECURE_COOKIES", "")
	if mustLoad(t).SecureCookies {
		t.Error("SecureCookies should default to false")
	}
	t.Setenv("ALMANAUT_SECURE_COOKIES", "true")
	if !mustLoad(t).SecureCookies {
		t.Error("SecureCookies should be true for \"true\"")
	}
}

func TestLoadReadsAuthCredentials(t *testing.T) {
	t.Setenv("ALMANAUT_AUTH_USER", "admin")
	t.Setenv("ALMANAUT_AUTH_PASS", "secret")
	cfg := mustLoad(t)
	if cfg.AuthUser != "admin" || cfg.AuthPass != "secret" {
		t.Fatalf("AuthUser=%q AuthPass=%q, want admin/secret", cfg.AuthUser, cfg.AuthPass)
	}
}

func TestLoadResetAdmin(t *testing.T) {
	t.Setenv("ALMANAUT_RESET_ADMIN", "")
	if mustLoad(t).ResetAdmin {
		t.Error("ResetAdmin should default to false")
	}
	t.Setenv("ALMANAUT_RESET_ADMIN", "true")
	if !mustLoad(t).ResetAdmin {
		t.Error("ResetAdmin should be true for \"true\"")
	}
}

func TestSecretFromFileTrimsTrailingNewline(t *testing.T) {
	// A file written with `echo secret > file` carries a trailing newline that
	// must not become part of the credential.
	path := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(path, []byte("s3cr3t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALMANAUT_AUTH_PASS_FILE", path)
	if got := mustLoad(t).AuthPass; got != "s3cr3t" {
		t.Errorf("AuthPass = %q, want %q", got, "s3cr3t")
	}
}

func TestSecretFileTakesPrecedenceOverEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALMANAUT_PROXMOX_TOKEN", "from-env")
	t.Setenv("ALMANAUT_PROXMOX_TOKEN_FILE", path)
	if got := mustLoad(t).ProxmoxToken; got != "from-file" {
		t.Errorf("ProxmoxToken = %q, want %q (file must win)", got, "from-file")
	}
}

func TestSecretFileUnreadableIsFatal(t *testing.T) {
	// A *_FILE that names a missing file is a misconfiguration; Load must error
	// rather than silently return an empty secret.
	t.Setenv("ALMANAUT_AUTH_PASS_FILE", filepath.Join(t.TempDir(), "does-not-exist"))
	if _, err := Load(); err == nil {
		t.Fatal("Load must error when *_FILE points to an unreadable file")
	}
}

func TestLoadNotifyDefaults(t *testing.T) {
	t.Setenv("ALMANAUT_NTFY_URL", "")
	t.Setenv("ALMANAUT_NOTIFY_WITHIN_DAYS", "")
	t.Setenv("ALMANAUT_NOTIFY_INTERVAL", "")
	c := mustLoad(t)
	if c.NtfyURL != "" {
		t.Errorf("NtfyURL = %q, want empty", c.NtfyURL)
	}
	if c.NotifyWithinDays != 30 {
		t.Errorf("NotifyWithinDays = %d, want 30", c.NotifyWithinDays)
	}
	if c.NotifyInterval != 24*time.Hour {
		t.Errorf("NotifyInterval = %v, want 24h", c.NotifyInterval)
	}
}

func TestLoadNotifyFromEnv(t *testing.T) {
	t.Setenv("ALMANAUT_NTFY_URL", "https://ntfy.sh/mylab")
	t.Setenv("ALMANAUT_NOTIFY_WITHIN_DAYS", "7")
	t.Setenv("ALMANAUT_NOTIFY_INTERVAL", "1h")
	c := mustLoad(t)
	if c.NtfyURL != "https://ntfy.sh/mylab" {
		t.Errorf("NtfyURL = %q", c.NtfyURL)
	}
	if c.NotifyWithinDays != 7 {
		t.Errorf("NotifyWithinDays = %d, want 7", c.NotifyWithinDays)
	}
	if c.NotifyInterval != time.Hour {
		t.Errorf("NotifyInterval = %v, want 1h", c.NotifyInterval)
	}
}

func TestGetenvIntAndDurationFallBackOnGarbage(t *testing.T) {
	t.Setenv("ALMANAUT_NOTIFY_WITHIN_DAYS", "not-a-number")
	t.Setenv("ALMANAUT_NOTIFY_INTERVAL", "nonsense")
	c := mustLoad(t)
	if c.NotifyWithinDays != 30 {
		t.Errorf("garbage days should fall back to 30, got %d", c.NotifyWithinDays)
	}
	if c.NotifyInterval != 24*time.Hour {
		t.Errorf("garbage interval should fall back to 24h, got %v", c.NotifyInterval)
	}
}

func TestLoadWebhookDefaults(t *testing.T) {
	t.Setenv("ALMANAUT_WEBHOOKS_ENABLED", "")
	t.Setenv("ALMANAUT_WEBHOOK_TIMEOUT", "")
	t.Setenv("ALMANAUT_WEBHOOK_MAX_ATTEMPTS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WebhooksEnabled {
		t.Errorf("WebhooksEnabled default = true, want false")
	}
	if cfg.WebhookTimeout != 10*time.Second {
		t.Errorf("WebhookTimeout default = %v, want 10s", cfg.WebhookTimeout)
	}
	if cfg.WebhookMaxAttempts != 5 {
		t.Errorf("WebhookMaxAttempts default = %d, want 5", cfg.WebhookMaxAttempts)
	}
}

func TestLoadWebhookOverrides(t *testing.T) {
	t.Setenv("ALMANAUT_WEBHOOKS_ENABLED", "true")
	t.Setenv("ALMANAUT_WEBHOOK_TIMEOUT", "3s")
	t.Setenv("ALMANAUT_WEBHOOK_MAX_ATTEMPTS", "2")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.WebhooksEnabled || cfg.WebhookTimeout != 3*time.Second || cfg.WebhookMaxAttempts != 2 {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoadKumaConfig(t *testing.T) {
	t.Setenv("ALMANAUT_KUMA_URL", "http://kuma.lan:3001")
	t.Setenv("ALMANAUT_KUMA_USER", "admin")
	t.Setenv("ALMANAUT_KUMA_PASS", "s3cret")
	t.Setenv("ALMANAUT_KUMA_INSECURE", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.KumaURL != "http://kuma.lan:3001" || cfg.KumaUser != "admin" || cfg.KumaPass != "s3cret" || !cfg.KumaInsecure {
		t.Fatalf("unexpected kuma config: %+v", cfg)
	}
}

func TestLoadKumaPassFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(path, []byte("filepass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALMANAUT_KUMA_PASS_FILE", path)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.KumaPass != "filepass" {
		t.Fatalf("KumaPass = %q, want filepass", cfg.KumaPass)
	}
}

func TestLoadLivenessDefaults(t *testing.T) {
	t.Setenv("ALMANAUT_LIVENESS_ENABLED", "")
	t.Setenv("ALMANAUT_LIVENESS_INTERVAL", "")
	t.Setenv("ALMANAUT_LIVENESS_TIMEOUT", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.LivenessEnabled {
		t.Fatal("liveness should default disabled")
	}
	if cfg.LivenessInterval != 60*time.Second {
		t.Fatalf("interval default = %v", cfg.LivenessInterval)
	}
	if cfg.LivenessTimeout != 5*time.Second {
		t.Fatalf("timeout default = %v", cfg.LivenessTimeout)
	}
}

func TestLoadLivenessOverrides(t *testing.T) {
	t.Setenv("ALMANAUT_LIVENESS_ENABLED", "true")
	t.Setenv("ALMANAUT_LIVENESS_INTERVAL", "30s")
	t.Setenv("ALMANAUT_LIVENESS_TIMEOUT", "2s")
	cfg, _ := Load()
	if !cfg.LivenessEnabled || cfg.LivenessInterval != 30*time.Second || cfg.LivenessTimeout != 2*time.Second {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadCertProbeDefaults(t *testing.T) {
	t.Setenv("ALMANAUT_CERT_PROBE_ENABLED", "")
	t.Setenv("ALMANAUT_CERT_PROBE_INTERVAL", "")
	t.Setenv("ALMANAUT_CERT_PROBE_TIMEOUT", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.CertProbeEnabled {
		t.Fatal("cert probe should default disabled")
	}
	if cfg.CertProbeInterval != 24*time.Hour {
		t.Fatalf("interval default = %v", cfg.CertProbeInterval)
	}
	if cfg.CertProbeTimeout != 10*time.Second {
		t.Fatalf("timeout default = %v", cfg.CertProbeTimeout)
	}
}

func TestLoadCertProbeOverrides(t *testing.T) {
	t.Setenv("ALMANAUT_CERT_PROBE_ENABLED", "true")
	t.Setenv("ALMANAUT_CERT_PROBE_INTERVAL", "6h")
	t.Setenv("ALMANAUT_CERT_PROBE_TIMEOUT", "3s")
	cfg, _ := Load()
	if !cfg.CertProbeEnabled || cfg.CertProbeInterval != 6*time.Hour || cfg.CertProbeTimeout != 3*time.Second {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}
