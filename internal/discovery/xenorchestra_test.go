package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

const xoHostFixture = `[{"id":"host-a","type":"host","name_label":"XCP node","power_state":"Running","address":"192.0.2.10","cpus":{"cores":8},"memory":{"size":17179869184},"href":"/rest/v0/hosts/host-a"}]`
const xoVMFixture = `[
 {"id":"vm-a","type":"VM","name_label":"Web","power_state":"Running","$container":"host-a","CPUs":{"number":4},"memory":{"size":4294967296},"addresses":{"0/ipv4/0":"192.0.2.20","1/ipv4/0":"192.0.2.20","0/ipv6/0":"2001:db8:0:0::1","bad":"not an IP"}},
 {"id":"vm-b","type":"VM","name_label":"Stopped","power_state":"Halted","$container":"pool-a"},
 {"id":"vm-c","type":"VM","name_label":"","power_state":"Running","$container":"host-outside-permissions"}
]`

func TestXenOrchestraResources(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		cookie, err := r.Cookie("authenticationToken")
		if err != nil || cookie.Value != "test-token" {
			t.Error("missing token cookie")
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("must not send a second authentication mechanism")
		}
		if r.URL.Query().Get("limit") != "" {
			t.Error("must request the complete collection")
		}
		if r.URL.Query().Get("fields") == "" {
			t.Error("missing object projection")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/xo/rest/v0/hosts":
			fmt.Fprint(w, xoHostFixture)
		case "/xo/rest/v0/vms":
			if !strings.Contains(r.URL.Query().Get("fields"), "$container") {
				t.Error("missing VM relationship projection")
			}
			fmt.Fprint(w, xoVMFixture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	c := NewXenOrchestraClient(ts.URL+"/xo/", "test-token", false)
	got, err := c.Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d resources", len(got))
	}
	wantHost := XenOrchestraResource{ID: "host-a", Type: "host", Name: "XCP node", Status: "running", CPU: 8, Memory: 17179869184, IPs: []string{"192.0.2.10"}}
	if !reflect.DeepEqual(got[0], wantHost) {
		t.Errorf("host = %+v", got[0])
	}
	wantVM := XenOrchestraResource{ID: "vm-a", Type: "vm", Name: "Web", HostID: "host-a", Status: "running", CPU: 4, Memory: 4294967296, IPs: []string{"192.0.2.20", "2001:db8::1"}}
	if !reflect.DeepEqual(got[1], wantVM) {
		t.Errorf("VM = %+v", got[1])
	}
	if got[2].HostID != "" || got[3].HostID != "" {
		t.Error("pool and inaccessible host must not become host links")
	}
	if got[3].Name != "vm-c" {
		t.Errorf("unnamed VM fallback = %q", got[3].Name)
	}
	if len(paths) != 2 {
		t.Errorf("requests = %v", paths)
	}
}

func TestXenOrchestraRejectsIncompleteOrMalformedResponses(t *testing.T) {
	cases := []struct{ name, hosts, vms string }{
		{"null collection", "null", "[]"},
		{"URL collection", `["/rest/v0/hosts/host-a"]`, "[]"},
		{"object instead of array", `{"items":[]}`, "[]"},
		{"missing ID", `[{"type":"host","name_label":"node"}]`, "[]"},
		{"missing type", `[{"id":"host-a"}]`, "[]"},
		{"null entry", "[null]", "[]"},
		{"truncated", xoHostFixture, `[{"id":"vm-a"`},
		{"trailing JSON", xoHostFixture, "[] []"},
		{"duplicate IDs", xoHostFixture, `[{"id":"host-a","type":"VM"}]`},
		{"wrong VM type", xoHostFixture, `[{"id":"vm-a","type":"VM-template"}]`},
		{"invalid field type", xoHostFixture, `[{"id":"vm-a","type":"VM","CPUs":{"number":"four"}}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/hosts") {
					fmt.Fprint(w, tc.hosts)
				} else {
					fmt.Fprint(w, tc.vms)
				}
			}))
			defer ts.Close()
			got, err := NewXenOrchestraClient(ts.URL, "token", false).Resources(context.Background())
			if err == nil || got != nil {
				t.Fatalf("partial inventory accepted: %v, %v", got, err)
			}
		})
	}
}

func TestXenOrchestraEmptyCollections(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[]") }))
	defer ts.Close()
	got, err := NewXenOrchestraClient(ts.URL, "token", false).Resources(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("empty inventory: %v, %v", got, err)
	}
}

func TestXenOrchestraAuthErrorsAndRedirects(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusFound, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			hits := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if strings.HasSuffix(r.URL.Path, "/hosts") {
					fmt.Fprint(w, xoHostFixture)
					return
				}
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(status)
				fmt.Fprint(w, "echoed secret-token")
			}))
			defer ts.Close()
			got, err := NewXenOrchestraClient(ts.URL, "secret-token", false).Resources(context.Background())
			if err == nil || got != nil {
				t.Fatalf("failed scan returned inventory: %v, %v", got, err)
			}
			if strings.Contains(err.Error(), "secret-token") {
				t.Fatal("response leaked token")
			}
			if !strings.Contains(err.Error(), fmt.Sprint(status)) {
				t.Errorf("status missing: %v", err)
			}
			if hits != 2 {
				t.Errorf("made %d requests, expected 2", hits)
			}
		})
	}
}

func TestXenOrchestraResponseBound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[]"+strings.Repeat(" ", 16<<20)) }))
	defer ts.Close()
	got, err := NewXenOrchestraClient(ts.URL, "token", false).Resources(context.Background())
	if err == nil || got != nil || !strings.Contains(err.Error(), "16 MiB") {
		t.Fatalf("oversized response: %v, %v", got, err)
	}
}

func TestXenOrchestraValidatesCredentialsBeforeRequest(t *testing.T) {
	for _, base := range []string{"", "file:///etc/passwd", "https://user:secret@example.org", "https://example.org?secret=x", "https://example.org#fragment"} {
		if _, err := NewXenOrchestraClient(base, "token", false).Resources(context.Background()); err == nil {
			t.Errorf("accepted URL %q", base)
		}
	}
	for _, token := range []string{"", "bad\nvalue", "bad;value"} {
		if _, err := NewXenOrchestraClient("http://127.0.0.1:1", token, false).Resources(context.Background()); err == nil || strings.Contains(err.Error(), "request:") {
			t.Errorf("invalid token not rejected before request: %v", err)
		}
	}
}

func TestXenOrchestraCancellationAndTLS(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[]") }))
	defer ts.Close()
	if _, err := NewXenOrchestraClient(ts.URL, "token", false).Resources(context.Background()); err == nil {
		t.Fatal("untrusted certificate accepted by default")
	}
	if _, err := NewXenOrchestraClient(ts.URL, "token", true).Resources(context.Background()); err != nil {
		t.Fatalf("explicit insecure mode failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := NewXenOrchestraClient(ts.URL, "token", true).Resources(ctx); err == nil {
		t.Fatal("cancelled scan succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation was not prompt")
	}
}

func TestProposeXenOrchestraHosts(t *testing.T) {
	res := []XenOrchestraResource{
		{ID: "vm-b", Type: "vm", Name: "Web", HostID: "node", CPU: 2, Memory: 4 << 30, IPs: []string{"192.0.2.20"}},
		{ID: "node", Type: "host", Name: "Node", CPU: 8, Memory: 16 << 30},
		{ID: "vm-a", Type: "vm", Name: " web "},
		{ID: "vm-c", Type: "vm", Name: "Database"},
	}
	got := ProposeXenOrchestraHosts(res, []domain.Host{{Name: " DATABASE "}})
	if len(got) != 4 || got[0].ID != "node" || got[1].ID != "vm-c" || got[2].ID != "vm-a" {
		t.Fatalf("wrong ordering: %+v", got)
	}
	if got[0].Host.Type != "physical" || got[0].Host.CPU != "8 cores" || got[0].Host.RAM != "16.0 GiB" {
		t.Errorf("host proposal = %+v", got[0])
	}
	if !got[1].AlreadyTracked || got[1].AmbiguousName {
		t.Errorf("existing match = %+v", got[1])
	}
	if !got[2].AmbiguousName || !got[3].AmbiguousName || got[3].AlreadyTracked {
		t.Error("duplicate names not distinguished from tracked records")
	}
	if got[3].HostID != "node" || got[3].Host.Type != "vm" || got[3].Host.RAM != "4.0 GiB" || !strings.Contains(got[3].Host.Notes, "vm-b") {
		t.Errorf("VM proposal = %+v", got[3])
	}
	got[3].Host.IPs[0] = "changed"
	if res[0].IPs[0] != "192.0.2.20" {
		t.Error("proposal mutated source IPs")
	}
}

type xenOrchestraRoundTripper func(*http.Request) (*http.Response, error)

func (f xenOrchestraRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestXenOrchestraTransportErrorRedactsTokenAndURL(t *testing.T) {
	c := NewXenOrchestraClient("https://xo.example.org/private-path", "secret-token", false)
	c.httpClient.Transport = xenOrchestraRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("malformed response secret-token: %w", context.DeadlineExceeded)
	})
	_, err := c.Resources(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "private-path") {
		t.Fatalf("transport error was not redacted: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("lost error classification: %v", err)
	}
}
