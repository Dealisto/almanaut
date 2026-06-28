// Package config loads runtime configuration from environment variables.
package config

import "os"

// Config holds the runtime configuration for the server.
type Config struct {
	Addr         string // TCP listen address, e.g. ":8080"
	DataDir      string // directory holding the SQLite database and exports
	DockerSocket string // path to the Docker Engine socket for discovery
}

// Load reads configuration from the environment, falling back to defaults.
func Load() Config {
	return Config{
		Addr:         getenv("ALMANAUT_ADDR", ":8080"),
		DataDir:      getenv("ALMANAUT_DATA_DIR", "./data"),
		DockerSocket: getenv("ALMANAUT_DOCKER_SOCKET", "/var/run/docker.sock"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
