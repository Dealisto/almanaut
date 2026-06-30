package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleResources = `{"data":[
{"type":"node","node":"pve","status":"online","maxcpu":8,"maxmem":33000000000,"maxdisk":100000000000,"id":"node/pve"},
{"type":"qemu","node":"pve","vmid":100,"name":"web","status":"running","maxcpu":4,"maxmem":4294967296,"maxdisk":34359738368,"id":"qemu/100"},
{"type":"lxc","node":"pve","vmid":101,"name":"dns","status":"running","maxcpu":1,"maxmem":536870912,"maxdisk":8589934592,"id":"lxc/101"},
{"type":"storage","node":"pve","storage":"local","status":"available","id":"storage/pve/local"}
]}`

func TestResourcesParsesAndFilters(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api2/json/cluster/resources" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleResources))
	}))
	defer ts.Close()

	c := NewProxmoxClient(ts.URL, "root@pam!tok=secret", false)
	res, err := c.Resources(context.Background())
	if err != nil {
		t.Fatalf("Resources: %v", err)
	}
	if gotAuth != "PVEAPIToken=root@pam!tok=secret" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if len(res) != 3 {
		t.Fatalf("kept %d resources, want 3 (storage filtered)", len(res))
	}
	if res[1].Type != "qemu" || res[1].Name != "web" || res[1].VMID != 100 ||
		res[1].MaxCPU != 4 || res[1].MaxMem != 4294967296 || res[1].ID != "qemu/100" {
		t.Errorf("qemu row decoded wrong: %+v", res[1])
	}
}

func TestResourcesNon200IncludesBodyExcerpt(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("authentication failure - invalid token value"))
	}))
	defer ts.Close()
	c := NewProxmoxClient(ts.URL, "bad", false)
	_, err := c.Resources(context.Background())
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if !strings.Contains(err.Error(), "invalid token value") {
		t.Errorf("error should include the response body excerpt, got: %v", err)
	}
}

func TestResourcesDoesNotFollowRedirect(t *testing.T) {
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		// A hostile/misconfigured endpoint trying to bounce the request
		// elsewhere; the client must not follow it.
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer ts.Close()
	c := NewProxmoxClient(ts.URL, "tok", false)
	if _, err := c.Resources(context.Background()); err == nil {
		t.Fatal("expected error when endpoint redirects, got nil")
	}
	if hits != 1 {
		t.Errorf("client made %d requests, want 1 (redirect must not be followed)", hits)
	}
}
