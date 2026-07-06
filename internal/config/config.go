// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
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
	AuthUser           string // ALMANAUT_AUTH_USER — username for the initial admin seeded at first startup
	AuthPass           string // ALMANAUT_AUTH_PASS — password for the initial admin (else a random one is logged)
	SecureCookies      bool   // force the Secure flag on cookies (set behind a TLS-terminating proxy)
	ResetAdmin         bool   // ALMANAUT_RESET_ADMIN — reset the admin password at startup (lockout recovery)

	NtfyURL          string        // ALMANAUT_NTFY_URL — ntfy topic URL; empty disables notifications
	NtfyToken        string        // ALMANAUT_NTFY_TOKEN — optional bearer for a protected topic
	NotifyWithinDays int           // ALMANAUT_NOTIFY_WITHIN_DAYS — "expiring soon" window
	NotifyInterval   time.Duration // ALMANAUT_NOTIFY_INTERVAL — how often the scheduler checks
}

// Load reads configuration from the environment, falling back to defaults. It
// returns an error only when a secret's *_FILE variant names a file that cannot
// be read — a misconfiguration that must not degrade to a silently empty secret.
func Load() (Config, error) {
	proxmoxToken, err := secretFromEnv("ALMANAUT_PROXMOX_TOKEN")
	if err != nil {
		return Config{}, err
	}
	authPass, err := secretFromEnv("ALMANAUT_AUTH_PASS")
	if err != nil {
		return Config{}, err
	}
	ntfyToken, err := secretFromEnv("ALMANAUT_NTFY_TOKEN")
	if err != nil {
		return Config{}, err
	}
	return Config{
		Addr:               getenv("ALMANAUT_ADDR", ":8080"),
		DataDir:            getenv("ALMANAUT_DATA_DIR", "./data"),
		DockerSocket:       getenv("ALMANAUT_DOCKER_SOCKET", "/var/run/docker.sock"),
		NetworkScanEnabled: getenvBool("ALMANAUT_ENABLE_NETWORK_SCAN", false),
		ScanSubnet:         getenv("ALMANAUT_SCAN_SUBNET", ""),
		ProxmoxURL:         getenv("ALMANAUT_PROXMOX_URL", ""),
		ProxmoxToken:       proxmoxToken,
		ProxmoxInsecure:    getenvBool("ALMANAUT_PROXMOX_INSECURE", false),
		AuthUser:           getenv("ALMANAUT_AUTH_USER", ""),
		AuthPass:           authPass,
		SecureCookies:      getenvBool("ALMANAUT_SECURE_COOKIES", false),
		ResetAdmin:         getenvBool("ALMANAUT_RESET_ADMIN", false),
		NtfyURL:            getenv("ALMANAUT_NTFY_URL", ""),
		NtfyToken:          ntfyToken,
		NotifyWithinDays:   getenvInt("ALMANAUT_NOTIFY_WITHIN_DAYS", 30),
		NotifyInterval:     getenvDuration("ALMANAUT_NOTIFY_INTERVAL", 24*time.Hour),
	}, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// secretFromEnv resolves a secret from KEY, or from the file named by KEY_FILE
// when that is set (the "_FILE" convention used by Docker/Kubernetes secrets).
// Reading from a file keeps the secret out of the process environment, so it
// does not leak via `docker inspect`, /proc, or inherited child processes.
// KEY_FILE takes precedence over KEY. A single trailing newline (as left by
// `echo secret > file`) is stripped.
func secretFromEnv(key string) (string, error) {
	if path := os.Getenv(key + "_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s_FILE: %w", key, err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	return os.Getenv(key), nil
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

// getenvInt reads a non-negative integer env var; unset, blank, unparseable, or
// negative values return def.
func getenvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

// getenvDuration reads a Go duration env var (e.g. "24h", "30m"); unset, blank,
// unparseable, or non-positive values return def.
func getenvDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}
