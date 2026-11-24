package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clearDiscoveryEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ALMANAUT_DISCOVERY_SOURCES_FILE", "ALMANAUT_DOCKER_SOCKET", "ALMANAUT_DISCOVERY_DOCKER_INTERVAL",
		"ALMANAUT_PROXMOX_URL", "ALMANAUT_PROXMOX_TOKEN", "ALMANAUT_PROXMOX_TOKEN_FILE", "ALMANAUT_PROXMOX_INSECURE", "ALMANAUT_DISCOVERY_PROXMOX_INTERVAL",
		"ALMANAUT_XO_URL", "ALMANAUT_XO_TOKEN", "ALMANAUT_XO_TOKEN_FILE", "ALMANAUT_XO_INSECURE", "ALMANAUT_DISCOVERY_XO_INTERVAL",
	} {
		t.Setenv(key, "")
	}
}

func sourceConfigFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sources.json")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALMANAUT_DISCOVERY_SOURCES_FILE", path)
	return path
}

func TestLegacyDiscoverySources(t *testing.T) {
	clearDiscoveryEnvironment(t)
	cfg := mustLoad(t)
	if len(cfg.DiscoverySources) != 1 || cfg.DiscoverySources[0].Key() != "docker:default" || cfg.DiscoverySources[0].Socket != "/var/run/docker.sock" {
		t.Fatalf("default source not preserved: %+v", cfg.DiscoverySources)
	}
	t.Setenv("ALMANAUT_DOCKER_SOCKET", "/run/podman/podman.sock")
	t.Setenv("ALMANAUT_DISCOVERY_DOCKER_INTERVAL", "3h")
	t.Setenv("ALMANAUT_PROXMOX_URL", "https://pve.example.test:8006")
	t.Setenv("ALMANAUT_PROXMOX_TOKEN", "pve-token")
	t.Setenv("ALMANAUT_PROXMOX_INSECURE", "true")
	t.Setenv("ALMANAUT_DISCOVERY_PROXMOX_INTERVAL", "1h")
	t.Setenv("ALMANAUT_XO_URL", "https://xo.example.test")
	t.Setenv("ALMANAUT_XO_TOKEN", "xo-token")
	t.Setenv("ALMANAUT_DISCOVERY_XO_INTERVAL", "2h")
	cfg = mustLoad(t)
	if len(cfg.DiscoverySources) != 3 {
		t.Fatalf("got %d sources, want 3", len(cfg.DiscoverySources))
	}
	wantKeys := []string{"docker:default", "proxmox:default", "xen-orchestra:default"}
	for i, source := range cfg.DiscoverySources {
		if source.Key() != wantKeys[i] {
			t.Errorf("source %d key = %q", i, source.Key())
		}
	}
	if cfg.DiscoverySources[0].Interval != 3*time.Hour || cfg.DiscoverySources[0].Socket != "/run/podman/podman.sock" || !cfg.DiscoverySources[1].Insecure || cfg.DiscoverySources[2].Token != "xo-token" {
		t.Fatal("legacy settings did not reach discovery sources")
	}
	t.Setenv("ALMANAUT_PROXMOX_TOKEN", "")
	if len(mustLoad(t).DiscoverySources) != 2 {
		t.Fatal("partially configured legacy source must remain disabled")
	}
}

func TestDiscoverySourceFileReplacesLegacySources(t *testing.T) {
	clearDiscoveryEnvironment(t)
	path := sourceConfigFile(t, `[
		{"id":"lab","name":"Lab cluster","type":"proxmox","url":"https://pve.example.test:8006/","token":"env-placeholder","token_file":"token","interval":"6h"},
		{"id":"lab","type":"xen-orchestra","url":"https://xo.example.test","token":"xo-token","insecure":true,"interval":"0"},
		{"id":"rootless","type":"podman","socket":"/run/user/1000/podman/podman.sock","interval":"30m"},
		{"id":"remote","type":"docker","socket":"/run/docker-remote.sock"}
	]`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "token"), []byte("file-token\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Replaced environment settings must not add a fifth source or read a stale
	// token file. Network scanning is independent of the engine source list.
	t.Setenv("ALMANAUT_PROXMOX_URL", "https://old.example.test")
	t.Setenv("ALMANAUT_PROXMOX_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("ALMANAUT_ENABLE_NETWORK_SCAN", "true")
	t.Setenv("ALMANAUT_SCAN_SUBNET", "192.0.2.0/24")
	cfg := mustLoad(t)
	if len(cfg.DiscoverySources) != 4 {
		t.Fatalf("got %d sources, want exactly file entries", len(cfg.DiscoverySources))
	}
	pve, xo, podman := cfg.DiscoverySources[0], cfg.DiscoverySources[1], cfg.DiscoverySources[2]
	if pve.Key() != "proxmox:lab" || pve.Name != "Lab cluster" || pve.URL != "https://pve.example.test:8006" || pve.Token != "file-token" || pve.Interval != 6*time.Hour {
		t.Fatal("file source or relative token_file not resolved correctly")
	}
	if xo.Name != "lab" || !xo.Insecure || xo.Interval != 0 || podman.Kind != "podman" || podman.Interval != 30*time.Minute {
		t.Fatal("source defaults or overrides incorrect")
	}
	if !cfg.NetworkScanEnabled || cfg.ScanSubnet != "192.0.2.0/24" {
		t.Fatal("explicit engine sources changed network discovery settings")
	}
}

func TestEmptyDiscoverySourceFileDisablesEngineSources(t *testing.T) {
	clearDiscoveryEnvironment(t)
	sourceConfigFile(t, "[]")
	cfg := mustLoad(t)
	if cfg.DiscoverySources == nil || len(cfg.DiscoverySources) != 0 {
		t.Fatal("explicit empty source list must remain non-nil and empty")
	}
}

func TestDiscoverySourceFileRejectsInvalidConfiguration(t *testing.T) {
	const credential = "do-not-include-this-token"
	cases := map[string]string{
		"not array":             `{"token":"` + credential + `"}`,
		"null":                  `null`,
		"trailing JSON":         `[] []`,
		"unknown field":         `[{"id":"lab","type":"docker","socket":"/run/docker.sock","` + credential + `":true}]`,
		"invalid field type":    `[{"id":"lab","type":"docker","socket":17,"token":"` + credential + `"}]`,
		"duplicate key":         `[{"id":"lab","type":"docker","socket":"/a"},{"id":"lab","type":"docker","socket":"/b"}]`,
		"invalid id":            `[{"id":"../` + credential + `","type":"docker","socket":"/run/docker.sock"}]`,
		"missing id":            `[{"type":"docker","socket":"/run/docker.sock"}]`,
		"missing socket":        `[{"id":"lab","type":"podman"}]`,
		"relative socket":       `[{"id":"lab","type":"podman","socket":"podman.sock"}]`,
		"unsupported kind":      `[{"id":"lab","type":"` + credential + `"}]`,
		"negative interval":     `[{"id":"lab","type":"docker","socket":"/run/docker.sock","interval":"-1s"}]`,
		"invalid interval":      `[{"id":"lab","type":"docker","socket":"/run/docker.sock","interval":"` + credential + `"}]`,
		"missing token":         `[{"id":"lab","type":"xen-orchestra","url":"https://xo.example.test"}]`,
		"missing url":           `[{"id":"lab","type":"proxmox","token":"` + credential + `"}]`,
		"url credentials":       `[{"id":"lab","type":"proxmox","url":"https://user:` + credential + `@pve.example.test","token":"token"}]`,
		"url query":             `[{"id":"lab","type":"xen-orchestra","url":"https://xo.example.test?token=` + credential + `","token":"token"}]`,
		"url fragment":          `[{"id":"lab","type":"xen-orchestra","url":"https://xo.example.test#` + credential + `","token":"token"}]`,
		"url scheme":            `[{"id":"lab","type":"xen-orchestra","url":"file:///tmp/` + credential + `","token":"token"}]`,
		"unreadable token file": `[{"id":"lab","type":"proxmox","url":"https://pve.example.test","token":"fallback","token_file":"missing-` + credential + `"}]`,
		"token newline":         `[{"id":"lab","type":"proxmox","url":"https://pve.example.test","token":"` + credential + `\nextra"}]`,
		"container token":       `[{"id":"lab","type":"docker","socket":"/run/docker.sock","token":"` + credential + `"}]`,
		"hypervisor socket":     `[{"id":"lab","type":"xen-orchestra","url":"https://xo.example.test","token":"` + credential + `","socket":"/run/docker.sock"}]`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			clearDiscoveryEnvironment(t)
			sourceConfigFile(t, input)
			_, err := Load()
			if err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if strings.Contains(err.Error(), credential) {
				t.Fatalf("configuration error exposed a credential: %v", err)
			}
		})
	}
}

func TestDiscoverySourceFileReadErrors(t *testing.T) {
	clearDiscoveryEnvironment(t)
	t.Setenv("ALMANAUT_DISCOVERY_SOURCES_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := Load(); err == nil {
		t.Fatal("missing source file must fail startup")
	}
	sourceConfigFile(t, strings.Repeat(" ", (1<<20)+1))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("oversized source file not bounded: %v", err)
	}
}

func TestDiscoverySourceDiagnosticsOmitToken(t *testing.T) {
	s := DiscoverySource{ID: "lab", Kind: "proxmox", Token: "secret-token", URL: "https://pve.example.test/private-path"}
	for _, diagnostic := range []string{fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprintf("%+v", []DiscoverySource{s})} {
		if strings.Contains(diagnostic, s.Token) || strings.Contains(diagnostic, "private-path") {
			t.Fatal("formatted source exposed credentials or endpoint path")
		}
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), s.Token) {
		t.Fatal("serialized source exposed token")
	}
}
