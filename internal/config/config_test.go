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
}
