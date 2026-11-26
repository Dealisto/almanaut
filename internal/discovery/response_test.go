package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInventoryClientsRejectMalformedCollections(t *testing.T) {
	for _, tc := range []struct {
		name, docker, proxmox string
	}{
		{"null", "null", "null"},
		{"missing collection", "{}", "{}"},
		{"null collection", "null", `{"data":null}`},
		{"trailing JSON", "[] []", `{"data":[]} {"data":[]}`},
		{"trailing garbage", "[] garbage", `{"data":[]} garbage`},
		{"null entry", "[null]", `{"data":[null]}`},
		{"empty entry", "[{}]", `{"data":[{}]}`},
		{"missing identity", `[{"Names":["/app"]}]`, `{"data":[{"type":"qemu","name":"app"}]}`},
		{"blank identity", `[{"Id":"  "}]`, `{"data":[{"type":"qemu","id":"  "}]}`},
		{"duplicate identity", `[{"Id":"one"},{"Id":"one"}]`, `{"data":[{"type":"qemu","id":"qemu/100"},{"type":"qemu","id":"qemu/100"}]}`},
		{"malformed after valid entry", `[{"Id":"one"},{}]`, `{"data":[{"type":"qemu","id":"qemu/100"},{}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				body := tc.docker
				if req.URL.Path == "/api2/json/cluster/resources" {
					body = tc.proxmox
				}
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			docker := &DockerClient{httpClient: srv.Client(), baseURL: srv.URL}
			if got, err := docker.Containers(context.Background()); err == nil || got != nil {
				t.Fatalf("Docker returned a successful or partial malformed scan: %+v, %v", got, err)
			}
			if got, err := NewProxmoxClient(srv.URL, "token", false).Resources(context.Background()); err == nil || got != nil {
				t.Fatalf("Proxmox returned a successful or partial malformed scan: %+v, %v", got, err)
			}
		})
	}
}

func TestDecodeInventoryEnforcesEntireResponseLimit(t *testing.T) {
	var result []any
	if err := decodeInventory(strings.NewReader("[]"+strings.Repeat(" ", 16<<20)), &result); err == nil {
		t.Fatal("oversized response with a valid JSON prefix was accepted")
	}
	if err := decodeInventory(strings.NewReader("[]\n"), &result); err != nil || result == nil {
		t.Fatalf("valid empty collection rejected: %v", err)
	}
}
