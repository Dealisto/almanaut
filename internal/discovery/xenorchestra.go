package discovery

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/redact"
)

// XenOrchestraResource is an inventory object from the Xen Orchestra REST API.
// Type is host or vm; HostID is set only when a VM's container is a known host.
type XenOrchestraResource struct {
	ID, Type, Name, HostID, Status string
	CPU                            int
	Memory                         int64
	IPs                            []string
}

// XenOrchestraClient reads XCP-ng/XenServer inventory through Xen Orchestra.
type XenOrchestraClient struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

// NewXenOrchestraClient accepts the XO base URL and an authentication token.
func NewXenOrchestraClient(baseURL, token string, insecure bool) *XenOrchestraClient {
	transport := &http.Transport{}
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &XenOrchestraClient{
		httpClient: &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: noRedirect},
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
	}
}

type xenOrchestraObject struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Name       string            `json:"name_label"`
	PowerState string            `json:"power_state"`
	Container  string            `json:"$container"`
	Address    string            `json:"address"`
	Addresses  map[string]string `json:"addresses"`
	HostCPUs   struct {
		Cores int `json:"cores"`
	} `json:"cpus"`
	VMCPUs struct {
		Number int `json:"number"`
	} `json:"CPUs"`
	Memory struct {
		Size int64 `json:"size"`
	} `json:"memory"`
}

// Resources fetches both complete collections. XO returns all visible objects
// when limit is omitted; fields makes collection entries objects instead of URLs.
// A failed or malformed collection never yields a partial inventory.
func (c *XenOrchestraClient) Resources(ctx context.Context) ([]XenOrchestraResource, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("xen orchestra URL must be an HTTP(S) base URL without credentials, query or fragment")
	}
	cookie := &http.Cookie{Name: "authenticationToken", Value: c.token}
	if c.token == "" || cookie.Valid() != nil {
		return nil, fmt.Errorf("xen orchestra token is empty or invalid")
	}

	hosts, err := c.collection(ctx, "hosts", "id,type,name_label,power_state,address,cpus,memory", cookie)
	if err != nil {
		return nil, err
	}
	vms, err := c.collection(ctx, "vms", "id,type,name_label,power_state,$container,addresses,CPUs,memory", cookie)
	if err != nil {
		return nil, err
	}
	knownHosts := make(map[string]bool, len(hosts))
	seen := make(map[string]bool, len(hosts)+len(vms))
	out := make([]XenOrchestraResource, 0, len(hosts)+len(vms))
	for _, group := range []struct {
		objects []xenOrchestraObject
		kind    string
	}{{hosts, "host"}, {vms, "VM"}} {
		for _, obj := range group.objects {
			if strings.TrimSpace(obj.ID) == "" || obj.Type != group.kind || seen[obj.ID] {
				return nil, fmt.Errorf("xen orchestra %s collection contains a missing/duplicate ID or unexpected object type", group.kind)
			}
			seen[obj.ID] = true
			name := strings.TrimSpace(obj.Name)
			if name == "" {
				name = obj.ID
			}
			r := XenOrchestraResource{
				ID: obj.ID, Type: strings.ToLower(obj.Type), Name: name,
				Status: strings.ToLower(obj.PowerState), Memory: obj.Memory.Size,
			}
			addresses := make([]string, 0, len(obj.Addresses)+1)
			if group.kind == "host" {
				knownHosts[obj.ID] = true
				r.CPU = obj.HostCPUs.Cores
				addresses = append(addresses, obj.Address)
			} else {
				r.CPU = obj.VMCPUs.Number
				// A halted VM may point to its pool; affinity is not residency.
				if knownHosts[obj.Container] {
					r.HostID = obj.Container
				}
				for _, ip := range obj.Addresses {
					addresses = append(addresses, ip)
				}
			}
			r.IPs = xenOrchestraIPs(addresses)
			out = append(out, r)
		}
	}
	return out, nil
}

func (c *XenOrchestraClient) collection(ctx context.Context, collection, fields string, cookie *http.Cookie) ([]xenOrchestraObject, error) {
	endpoint := c.baseURL + "/rest/v0/" + collection + "?" + url.Values{"fields": {fields}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("xen orchestra: could not build inventory request")
	}
	req.AddCookie(cookie)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xen orchestra %s request: %w", collection, c.safeError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Do not include response bodies: an auth gateway may echo the cookie.
		return nil, fmt.Errorf("xen orchestra %s API status %d", collection, resp.StatusCode)
	}
	const maxBody = 16 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read xen orchestra %s response: %w", collection, c.safeError(err))
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("xen orchestra %s response exceeds 16 MiB", collection)
	}
	var objects []xenOrchestraObject
	if err := json.Unmarshal(body, &objects); err != nil || objects == nil {
		return nil, fmt.Errorf("xen orchestra %s response must be a JSON array of inventory objects", collection)
	}
	return objects, nil
}

// Transport errors can contain malformed server responses as well as URLs.
// Preserve error classification while keeping reflected credentials out of logs.
func (c *XenOrchestraClient) safeError(err error) error {
	err = redact.Error(err)
	return &xenOrchestraError{err: err, message: strings.ReplaceAll(err.Error(), c.token, "[redacted]")}
}

type xenOrchestraError struct {
	err     error
	message string
}

func (e *xenOrchestraError) Error() string { return e.message }
func (e *xenOrchestraError) Unwrap() error { return e.err }

func xenOrchestraIPs(addresses []string) []string {
	seen := make(map[string]bool, len(addresses))
	var ips []string
	for _, address := range addresses {
		if ip := net.ParseIP(strings.TrimSpace(address)); ip != nil {
			canonical := ip.String()
			if !seen[canonical] {
				seen[canonical] = true
				ips = append(ips, canonical)
			}
		}
	}
	sort.Slice(ips, func(i, j int) bool { return ipLess(ips[i], ips[j]) })
	return ips
}
