// Package agent collects host facts and reports them to an almanaut server.
package agent

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// DefaultConfigPath is where the packaged systemd unit expects the config.
const DefaultConfigPath = "/etc/almanaut-agent/config.toml"

// TokenEnvVar overrides the config file's token, so a deployment can inject
// the secret from systemd credentials or a secret store instead of leaving it
// on disk.
const TokenEnvVar = "ALMANAUT_AGENT_TOKEN"

// Config is everything the agent needs to reach its server.
type Config struct {
	ServerURL string
	Token     string
}

// LoadConfig reads path and applies the environment override for the token.
//
// The parser accepts only `key = "value"` lines, blank lines and # comments,
// and rejects anything else. That is a deliberate subset of TOML: with two
// settings a full parser would be a dependency for nothing, and rejecting
// unrecognized syntax means an operator who writes a real TOML table gets an
// error rather than a setting that silently did not apply.
func LoadConfig(path string, getenv func(string) string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	seen := make(map[string]int) // key -> line number
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		key, value, err := parseConfigLine(sc.Text())
		if err != nil {
			return Config{}, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		switch key {
		case "":
			// blank or comment
		case "server_url":
			if prevLine, exists := seen[key]; exists {
				return Config{}, fmt.Errorf("%s line %d: duplicate key %q (first seen at line %d)", path, line, key, prevLine)
			}
			seen[key] = line
			cfg.ServerURL = value
		case "token":
			if prevLine, exists := seen[key]; exists {
				return Config{}, fmt.Errorf("%s line %d: duplicate key %q (first seen at line %d)", path, line, key, prevLine)
			}
			seen[key] = line
			cfg.Token = value
		default:
			return Config{}, fmt.Errorf("%s line %d: unknown key %q", path, line, key)
		}
	}
	if err := sc.Err(); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	if v := getenv(TokenEnvVar); v != "" {
		cfg.Token = v
	}
	if cfg.ServerURL == "" {
		return Config{}, fmt.Errorf("%s: server_url is required", path)
	}
	if cfg.Token == "" {
		return Config{}, fmt.Errorf("%s: token is required (or set %s)", path, TokenEnvVar)
	}
	return cfg, nil
}

// parseConfigLine returns the key and value of one config line. A blank line
// or a comment yields an empty key and no error. After the closing quote,
// only whitespace and # comments are permitted. Backslashes in values are
// rejected to prevent confusion about escape sequences.
func parseConfigLine(raw string) (key, value string, err error) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", nil
	}

	// Detect multi-line string opener """ early for a better error message.
	if strings.Contains(line, `"""`) {
		return "", "", fmt.Errorf("multi-line strings are not supported")
	}

	k, v, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", fmt.Errorf("expected `key = \"value\"`, got %q", raw)
	}
	key = strings.TrimSpace(k)
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, `"`) {
		return "", "", fmt.Errorf("value for %q must be double-quoted", key)
	}

	// Find closing quote and extract value.
	end := strings.Index(v[1:], `"`)
	if end < 0 {
		return "", "", fmt.Errorf("value for %q is missing its closing quote", key)
	}
	valueContent := v[1 : 1+end]

	// Reject backslashes in the value (no escape sequences supported).
	if strings.Contains(valueContent, `\`) {
		return "", "", fmt.Errorf("escape sequences are not supported in value for %q", key)
	}

	// Check what follows the closing quote: only whitespace or # comment allowed.
	afterQuote := strings.TrimSpace(v[1+end+1:])
	if afterQuote != "" && !strings.HasPrefix(afterQuote, "#") {
		return "", "", fmt.Errorf("unexpected text after value for %q: %q (only comments allowed)", key, afterQuote)
	}

	return key, valueContent, nil
}
