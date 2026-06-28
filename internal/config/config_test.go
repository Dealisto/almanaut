package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("ALMANAUT_ADDR", "")
	t.Setenv("ALMANAUT_DATA_DIR", "")
	c := Load()
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want \":8080\"", c.Addr)
	}
	if c.DataDir != "./data" {
		t.Errorf("DataDir = %q, want \"./data\"", c.DataDir)
	}
	t.Setenv("ALMANAUT_DOCKER_SOCKET", "")
	if c := Load(); c.DockerSocket != "/var/run/docker.sock" {
		t.Errorf("DockerSocket = %q, want \"/var/run/docker.sock\"", c.DockerSocket)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("ALMANAUT_ADDR", ":9000")
	t.Setenv("ALMANAUT_DATA_DIR", "/var/almanaut")
	c := Load()
	if c.Addr != ":9000" {
		t.Errorf("Addr = %q, want \":9000\"", c.Addr)
	}
	if c.DataDir != "/var/almanaut" {
		t.Errorf("DataDir = %q, want \"/var/almanaut\"", c.DataDir)
	}
	t.Setenv("ALMANAUT_DOCKER_SOCKET", "/tmp/docker.sock")
	if c := Load(); c.DockerSocket != "/tmp/docker.sock" {
		t.Errorf("DockerSocket = %q, want \"/tmp/docker.sock\"", c.DockerSocket)
	}
}

func TestLoadNetworkScanDefaults(t *testing.T) {
	t.Setenv("ALMANAUT_ENABLE_NETWORK_SCAN", "")
	t.Setenv("ALMANAUT_SCAN_SUBNET", "")
	c := Load()
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
	c := Load()
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
		if got := Load().NetworkScanEnabled; got != want {
			t.Errorf("value %q -> %v, want %v", v, got, want)
		}
	}
}
