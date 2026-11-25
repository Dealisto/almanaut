package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartupDataDirectoryPermissions(t *testing.T) {
	if os.Getenv("ALMANAUT_TEST_DATA_PERMISSIONS_PROCESS") == "1" {
		main()
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions are not available on Windows")
	}
	for _, existing := range []bool{false, true} {
		name := "new"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			want := os.FileMode(0o700)
			if existing {
				want = 0o750
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, want); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStartupDataDirectoryPermissions$")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "ALMANAUT_") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "ALMANAUT_TEST_DATA_PERMISSIONS_PROCESS=1", "ALMANAUT_DATA_DIR="+dir, "ALMANAUT_ADDR=127.0.0.1:0", "ALMANAUT_AUTH_USER=admin", "ALMANAUT_AUTH_PASS=test-password-123")
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				if t.Failed() {
					t.Logf("startup output: %s", output.String())
				}
			}()
			for {
				if _, err := os.Stat(filepath.Join(dir, "almanaut.db")); err == nil {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("server did not create its database")
				case <-time.After(10 * time.Millisecond):
				}
			}
			info, err := os.Stat(dir)
			if err != nil || info.Mode().Perm() != want {
				t.Fatalf("data directory permissions: info=%v err=%v, want %04o", info, err, want)
			}
		})
	}
}

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

func TestHealthcheckRejectsRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var redirected atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/healthz" {
					http.Redirect(w, r, "/login", status)
					return
				}
				redirected.Store(true)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			if got := runHealthcheck(server.Listener.Addr().String()); got != 1 {
				t.Errorf("healthcheck returned %d for redirect to a healthy page", got)
			}
			if redirected.Load() {
				t.Error("healthcheck followed a redirect")
			}
		})
	}
}
