package discovery

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/redact"
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
		httpClient: &http.Client{
			Timeout:       15 * time.Second,
			Transport:     transport,
			CheckRedirect: noRedirect,
		},
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
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
		return nil, fmt.Errorf("build proxmox request: %w", c.safeError(err))
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxmox request: %w", c.safeError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// An authentication gateway may echo all or part of the API token.
		// Match Xen Orchestra by keeping response bodies out of visible errors.
		return nil, fmt.Errorf("proxmox API status %d", resp.StatusCode)
	}
	var body struct {
		Data []apiResource `json:"data"`
	}
	if err := decodeInventory(resp.Body, &body); err != nil {
		return nil, fmt.Errorf("decode resources: %w", c.safeError(err))
	}
	if body.Data == nil {
		return nil, fmt.Errorf("proxmox response must contain a data array")
	}
	out := make([]ProxmoxResource, 0, len(body.Data))
	seen := make(map[string]bool, len(body.Data))
	for _, r := range body.Data {
		if strings.TrimSpace(r.Type) == "" {
			return nil, fmt.Errorf("proxmox response contains a missing resource type")
		}
		switch r.Type {
		case "node", "qemu", "lxc":
		default:
			continue
		}
		if strings.TrimSpace(r.ID) == "" || seen[r.ID] {
			return nil, fmt.Errorf("proxmox response contains a missing or duplicate resource ID")
		}
		seen[r.ID] = true
		out = append(out, ProxmoxResource(r))
	}
	return out, nil
}

func (c *ProxmoxClient) safeError(err error) error {
	_, secret, _ := strings.Cut(c.token, "=")
	return redact.ErrorWithURL(err, c.baseURL, c.token, secret)
}

// noRedirect tells an http.Client not to follow redirects. The discovery REST
// endpoints (Proxmox, Docker) have no legitimate reason to redirect, so a 30x
// is more likely a misconfiguration or a hostile endpoint trying to bounce the
// request at an internal address; surface the redirect response as-is instead.
func noRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// statusError builds an error for a non-OK API response, including a bounded
// excerpt of the body so the server's own explanation (e.g. an invalid-token
// message on a 401/403) reaches the operator instead of a bare status code.
func statusError(source string, resp *http.Response) error {
	const maxExcerpt = 512
	excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, maxExcerpt))
	if msg := strings.TrimSpace(string(excerpt)); msg != "" {
		return fmt.Errorf("%s API status %d: %s", source, resp.StatusCode, msg)
	}
	return fmt.Errorf("%s API status %d", source, resp.StatusCode)
}
