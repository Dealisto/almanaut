package discovery

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProxmoxResource is one node/VM/container row from /cluster/resources.
type ProxmoxResource struct {
	Type    string
	Node    string
	Name    string
	Status  string
	ID      string
	VMID    int
	MaxCPU  int
	MaxMem  int64
	MaxDisk int64
}

// ProxmoxClient queries the Proxmox VE API read-only with an API token.
// httpClient/baseURL/token are unexported but settable within the package so
// tests can target an httptest.Server.
type ProxmoxClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

// NewProxmoxClient returns a client for the Proxmox API at baseURL (e.g.
// "https://pve.lan:8006"). token is "user@realm!tokenid=secret". When insecure
// is true the client skips TLS verification (Proxmox ships a self-signed cert).
func NewProxmoxClient(baseURL, token string, insecure bool) *ProxmoxClient {
	transport := &http.Transport{}
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &ProxmoxClient{
		httpClient: &http.Client{Timeout: 15 * time.Second, Transport: transport},
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
	}
}

type apiResource struct {
	Type    string `json:"type"`
	Node    string `json:"node"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	ID      string `json:"id"`
	VMID    int    `json:"vmid"`
	MaxCPU  int    `json:"maxcpu"`
	MaxMem  int64  `json:"maxmem"`
	MaxDisk int64  `json:"maxdisk"`
}

// Resources lists cluster nodes, VMs, and LXC containers via
// GET /api2/json/cluster/resources (works on a standalone node too).
func (c *ProxmoxClient) Resources(ctx context.Context) ([]ProxmoxResource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api2/json/cluster/resources", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxmox request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxmox API status %d", resp.StatusCode)
	}
	var body struct {
		Data []apiResource `json:"data"`
	}
	// Cap the response so a buggy or hostile endpoint cannot exhaust memory; a
	// real cluster inventory is far below this.
	const maxBody = 16 << 20 // 16 MiB
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode resources: %w", err)
	}
	out := make([]ProxmoxResource, 0, len(body.Data))
	for _, r := range body.Data {
		switch r.Type {
		case "node", "qemu", "lxc":
		default:
			continue
		}
		out = append(out, ProxmoxResource{
			Type: r.Type, Node: r.Node, Name: r.Name, Status: r.Status,
			ID: r.ID, VMID: r.VMID, MaxCPU: r.MaxCPU, MaxMem: r.MaxMem, MaxDisk: r.MaxDisk,
		})
	}
	return out, nil
}
