package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHypervisorClientsRedactReflectedEndpointPaths(t *testing.T) {
	for _, kind := range []string{"proxmox", "xen-orchestra"} {
		for _, segment := range []string{"PRIVATE-path", "private%22PRIVATE-path"} {
			t.Run(kind+"/"+segment, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, buffer, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					_, _ = fmt.Fprintf(buffer, "invalid %s\r\n\r\n", r.URL.Path)
					_ = buffer.Flush()
				}))
				defer srv.Close()
				var err error
				endpoint := srv.URL + "/" + segment
				if kind == "proxmox" {
					_, err = NewProxmoxClient(endpoint, "user@pam!inventory=secret", false).Resources(context.Background())
				} else {
					_, err = NewXenOrchestraClient(endpoint, "secret", false).Resources(context.Background())
				}
				if err == nil || strings.Contains(err.Error(), "PRIVATE") || !strings.Contains(err.Error(), "malformed HTTP") {
					t.Fatalf("missing or unsafe protocol error: %v", err)
				}
			})
		}
	}
}

func TestHypervisorSafeErrorsPreserveTimeout(t *testing.T) {
	for name, sanitize := range map[string]func(error) error{
		"proxmox":       NewProxmoxClient("https://example.test/PRIVATE-path", "user@pam!inventory=secret", false).safeError,
		"xen-orchestra": NewXenOrchestraClient("https://example.test/PRIVATE-path", "secret", false).safeError,
	} {
		t.Run(name, func(t *testing.T) {
			err := sanitize(context.DeadlineExceeded)
			timeout, ok := err.(interface{ Timeout() bool })
			if !errors.Is(err, context.DeadlineExceeded) || !ok || !timeout.Timeout() {
				t.Fatalf("redaction lost timeout identity: %v", err)
			}
		})
	}
}
