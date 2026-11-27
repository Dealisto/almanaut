package kuma

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionOperationsRedactCredentials(t *testing.T) {
	operations := map[string]func(*Session, context.Context) error{
		"add": func(s *Session, ctx context.Context) error {
			_, err := s.Add(ctx, Monitor{Name: "service", URL: "https://service.example"})
			return err
		},
		"edit": func(s *Session, ctx context.Context) error {
			return s.Edit(ctx, Monitor{ID: 1, Name: "service", URL: "https://service.example"})
		},
		"delete": func(s *Session, ctx context.Context) error { return s.Delete(ctx, 1) },
	}
	for operation, run := range operations {
		for _, mode := range []string{"refused", "malformed", "long-password", "endpoint", "cancelled"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				f := newFakeKuma(t)
				f.user = "PRIVATE-user"
				f.pass = `PRIVATE-"password`
				if mode == "long-password" {
					f.pass += strings.Repeat("x", 300)
				}
				message := "operation refused for " + f.user + " password=" + f.pass
				baseURL := f.url()
				if mode == "endpoint" {
					secret := "PRIVATE-path-" + strings.Repeat("x", 250)
					proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						r.URL.Path = "/socket.io/"
						f.handle(w, r)
					}))
					t.Cleanup(proxy.Close)
					baseURL = proxy.URL + "/" + secret
					message = "operation refused for endpoint " + secret
				}
				var raw []byte
				if mode == "malformed" {
					raw, _ = json.Marshal(message) // Valid JSON, but not an acknowledgement object.
				} else {
					raw, _ = json.Marshal(map[string]any{"ok": false, "msg": message})
				}
				f.operationAckOverride = string(raw)
				s, err := NewClient(baseURL, f.user, f.pass, false).Connect(testCtx(t))
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				ctx := testCtx(t)
				if mode == "cancelled" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				err = run(s, ctx)
				if err == nil || strings.Contains(err.Error(), "PRIVATE") {
					t.Fatalf("unsafe or missing error: %v", err)
				}
				if mode == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation identity lost: %v", err)
				}
			})
		}
	}
}
