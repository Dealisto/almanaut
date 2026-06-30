// Package discovery proposes entities discovered from external sources (e.g. a
// Docker socket) for the user to confirm. It never mutates the source.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Port is a published container port mapping.
type Port struct {
	Public  int
	Private int
	Proto   string
}

// Container is a running Docker container discovered via the Engine API.
type Container struct {
	ID             string
	Name           string
	Image          string
	State          string
	ComposeProject string
	Ports          []Port
}

// DockerClient queries the Docker Engine API. httpClient and baseURL are
// unexported but settable within the package so tests can target an
// httptest.Server instead of a real unix socket.
type DockerClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewSocketClient returns a DockerClient that dials the unix socket at path.
func NewSocketClient(path string) *DockerClient {
	return &DockerClient{
		httpClient: &http.Client{
			// Bound the whole request like the Proxmox client: a daemon that
			// accepts the connection then stalls must not hold the goroutine open.
			Timeout:       15 * time.Second,
			CheckRedirect: noRedirect,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", path)
				},
			},
		},
		baseURL: "http://docker",
	}
}

type apiContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
	Ports  []apiPort         `json:"Ports"`
}

type apiPort struct {
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

// Containers lists running containers via GET /containers/json.
func (c *DockerClient) Containers(ctx context.Context) ([]Container, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/containers/json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusError("docker", resp)
	}
	var raw []apiContainer
	// Cap the response so a hostile or malfunctioning endpoint cannot exhaust
	// memory; a real container list is far below this (matches the Proxmox cap).
	const maxBody = 16 << 20 // 16 MiB
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode containers: %w", err)
	}
	out := make([]Container, 0, len(raw))
	for _, rc := range raw {
		out = append(out, Container{
			ID:             rc.ID,
			Name:           containerName(rc.Names),
			Image:          rc.Image,
			State:          rc.State,
			ComposeProject: rc.Labels["com.docker.compose.project"],
			Ports:          publishedPorts(rc.Ports),
		})
	}
	return out, nil
}

func containerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func publishedPorts(ports []apiPort) []Port {
	seen := map[string]bool{}
	var out []Port
	for _, p := range ports {
		if p.PublicPort == 0 {
			continue
		}
		key := fmt.Sprintf("%d:%d/%s", p.PublicPort, p.PrivatePort, p.Type)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Port{Public: p.PublicPort, Private: p.PrivatePort, Proto: p.Type})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Public != out[j].Public {
			return out[i].Public < out[j].Public
		}
		return out[i].Private < out[j].Private
	})
	return out
}
