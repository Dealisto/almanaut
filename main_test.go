package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestHealthcheckIgnoresStartupFiles(t *testing.T) {
	if os.Getenv("ALMANAUT_TEST_HEALTHCHECK_PROCESS") == "1" {
		os.Args = []string{"almanaut", "healthcheck"}
		main()
		return
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	for _, key := range []string{"ALMANAUT_AUTH_PASS_FILE", "ALMANAUT_DISCOVERY_SOURCES_FILE"} {
		t.Run(key, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHealthcheckIgnoresStartupFiles$")
			cmd.Env = append(os.Environ(),
				"ALMANAUT_TEST_HEALTHCHECK_PROCESS=1",
				"ALMANAUT_ADDR="+server.Listener.Addr().String(),
				key+"="+filepath.Join(t.TempDir(), "missing"),
			)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("healthy server rejected because of startup configuration: %v\n%s", err, output)
			}
		})
	}
}

func TestHealthcheckUsesListeningAddress(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "127.0.0.2", "::1", "0.0.0.0", "::"} {
		t.Run(host, func(t *testing.T) {
			listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
			if err != nil {
				t.Skipf("listen on %s unavailable: %v", host, err)
			}
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Errorf("unexpected probe path: %s", r.URL.Path)
				}
				w.WriteHeader(http.StatusOK)
			}))
			server.Listener.Close()
			server.Listener = listener
			server.Start()
			defer server.Close()
			if got := runHealthcheck(listener.Addr().String()); got != 0 {
				t.Fatalf("healthcheck returned %d for healthy listener %s", got, listener.Addr())
			}
		})
	}
}

func TestHealthcheckRejectsUnhealthyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	if got := runHealthcheck(server.Listener.Addr().String()); got != 1 {
		t.Fatalf("healthcheck returned %d for unhealthy server", got)
	}
}
