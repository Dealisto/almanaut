// Package config loads runtime configuration from environment variables.
package config

import (
	"os"
	"strings"
)

// Config holds the runtime configuration for the server.
type Config struct {
	Addr               string // TCP listen address, e.g. ":8080"
	DataDir            string // directory holding the SQLite database and exports
	DockerSocket       string // path to the Docker Engine socket for discovery
	NetworkScanEnabled bool   // whether the opt-in subnet scan is exposed
	ScanSubnet         string // default subnet (CIDR) pre-filled in the scan form
	ProxmoxURL         string // Proxmox VE API base URL, e.g. https://pve.lan:8006
	ProxmoxToken       string // Proxmox API token "user@realm!tokenid=secret"
	ProxmoxInsecure    bool   // skip TLS verification (self-signed Proxmox cert)
}

// Load reads configuration from the environment, falling back to defaults.
func Load() Config {
	return Config{
		Addr:               getenv("ALMANAUT_ADDR", ":8080"),
		DataDir:            getenv("ALMANAUT_DATA_DIR", "./data"),
		DockerSocket:       getenv("ALMANAUT_DOCKER_SOCKET", "/var/run/docker.sock"),
		NetworkScanEnabled: getenvBool("ALMANAUT_ENABLE_NETWORK_SCAN", false),
		ScanSubnet:         getenv("ALMANAUT_SCAN_SUBNET", ""),
		ProxmoxURL:         getenv("ALMANAUT_PROXMOX_URL", ""),
		ProxmoxToken:       getenv("ALMANAUT_PROXMOX_TOKEN", ""),
		ProxmoxInsecure:    getenvBool("ALMANAUT_PROXMOX_INSECURE", false),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getenvBool reads a boolean env var. "1", "true", "yes" (any case) are true;
// anything else non-empty is false; unset returns def.
func getenvBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes"
}
