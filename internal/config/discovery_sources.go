package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DiscoverySource describes one engine or hypervisor connection. ID is stable
// across endpoint changes and supplies the namespace for imported identities.
type DiscoverySource struct {
	ID       string
	Name     string
	Kind     string
	URL      string
	Token    string `json:"-" yaml:"-"`
	Socket   string
	Insecure bool
	Interval time.Duration
}

func (s DiscoverySource) Key() string { return s.Kind + ":" + s.ID }

// String and GoString keep credentials and endpoint paths out of diagnostic
// formatting. Tokens are also excluded from JSON/YAML serialization.
func (s DiscoverySource) String() string {
	return fmt.Sprintf("DiscoverySource{Key:%q Interval:%s}", s.Key(), s.Interval)
}
func (s DiscoverySource) GoString() string { return s.String() }

type discoverySourceFile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"type"`
	URL       string `json:"url"`
	Token     string `json:"token"`
	TokenFile string `json:"token_file"`
	Socket    string `json:"socket"`
	Insecure  bool   `json:"insecure"`
	Interval  string `json:"interval"`
}

var discoverySourceID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func loadDiscoverySources(path string, cfg Config) ([]DiscoverySource, error) {
	if path == "" {
		return legacyDiscoverySources(cfg), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read ALMANAUT_DISCOVERY_SOURCES_FILE: cannot open source file")
	}
	defer f.Close()
	const maxSourceFile = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxSourceFile+1))
	if err != nil {
		return nil, fmt.Errorf("read ALMANAUT_DISCOVERY_SOURCES_FILE: cannot read source file")
	}
	if len(data) > maxSourceFile {
		return nil, fmt.Errorf("ALMANAUT_DISCOVERY_SOURCES_FILE exceeds 1 MiB")
	}
	var rows []discoverySourceFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rows); err != nil || rows == nil {
		return nil, fmt.Errorf("ALMANAUT_DISCOVERY_SOURCES_FILE must contain a JSON array with supported source fields")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("ALMANAUT_DISCOVERY_SOURCES_FILE must contain exactly one JSON array")
	}
	out := make([]DiscoverySource, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for i, row := range rows {
		s, err := row.resolve(filepath.Dir(path))
		if err != nil {
			return nil, fmt.Errorf("discovery source %d: %w", i+1, err)
		}
		if seen[s.Key()] {
			return nil, fmt.Errorf("discovery source %d: duplicate type and id", i+1)
		}
		seen[s.Key()] = true
		out = append(out, s)
	}
	return out, nil
}

func (row discoverySourceFile) resolve(baseDir string) (DiscoverySource, error) {
	s := DiscoverySource{
		ID: row.ID, Name: strings.TrimSpace(row.Name), Kind: row.Kind,
		URL: strings.TrimRight(strings.TrimSpace(row.URL), "/"), Token: row.Token,
		Socket: row.Socket, Insecure: row.Insecure,
	}
	if !discoverySourceID.MatchString(s.ID) {
		return s, fmt.Errorf("id must be 1–64 lowercase letters, digits, underscores or hyphens, starting with a letter or digit")
	}
	if s.Name == "" {
		s.Name = s.ID
	}
	if row.Interval != "" {
		d, err := time.ParseDuration(row.Interval)
		if err != nil || d < 0 {
			return s, fmt.Errorf("interval must be zero or a positive Go duration")
		}
		s.Interval = d
	}
	switch s.Kind {
	case "docker", "podman":
		if s.Socket == "" || !filepath.IsAbs(s.Socket) {
			return s, fmt.Errorf("container sources require an absolute socket path")
		}
		if row.URL != "" || row.Token != "" || row.TokenFile != "" || row.Insecure {
			return s, fmt.Errorf("container sources do not accept url, token, token_file or insecure")
		}
	case "proxmox", "xen-orchestra":
		if row.Socket != "" {
			return s, fmt.Errorf("hypervisor sources do not accept socket")
		}
		u, err := url.Parse(s.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return s, fmt.Errorf("url must be an HTTP(S) base URL without credentials, query or fragment")
		}
		if row.TokenFile != "" {
			path := row.TokenFile
			if !filepath.IsAbs(path) {
				path = filepath.Join(baseDir, path)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return s, fmt.Errorf("cannot read token_file")
			}
			s.Token = strings.TrimRight(string(b), "\r\n")
		}
		if strings.TrimSpace(s.Token) == "" || strings.ContainsAny(s.Token, "\r\n") {
			return s, fmt.Errorf("a nonempty token or token_file without embedded newlines is required")
		}
	default:
		return s, fmt.Errorf("type must be docker, podman, proxmox or xen-orchestra")
	}
	return s, nil
}

func legacyDiscoverySources(cfg Config) []DiscoverySource {
	sources := []DiscoverySource{{
		ID: "default", Name: "Docker / Podman", Kind: "docker",
		Socket: cfg.DockerSocket, Interval: cfg.DiscoveryDockerInterval,
	}}
	if cfg.ProxmoxURL != "" && cfg.ProxmoxToken != "" {
		sources = append(sources, DiscoverySource{
			ID: "default", Name: "Proxmox", Kind: "proxmox", URL: cfg.ProxmoxURL,
			Token: cfg.ProxmoxToken, Insecure: cfg.ProxmoxInsecure, Interval: cfg.DiscoveryProxmoxInterval,
		})
	}
	if cfg.XenOrchestraURL != "" && cfg.XenOrchestraToken != "" {
		sources = append(sources, DiscoverySource{
			ID: "default", Name: "XCP-ng / Xen Orchestra", Kind: "xen-orchestra", URL: cfg.XenOrchestraURL,
			Token: cfg.XenOrchestraToken, Insecure: cfg.XenOrchestraInsecure, Interval: cfg.DiscoveryXenOrchestraInterval,
		})
	}
	return sources
}
