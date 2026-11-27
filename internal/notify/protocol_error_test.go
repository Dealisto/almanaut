package notify

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendRedactsMalformedHTTPResponse(t *testing.T) {
	for _, kind := range []string{"ntfy", "discord"} {
		for _, reflected := range []string{"path", "token", "query", "quoted token"} {
			t.Run(kind+"/"+reflected, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, buffer, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					value := r.URL.Path
					if reflected == "token" {
						value = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
						if kind == "discord" {
							value = "PRIVATE-token"
						}
					} else if reflected == "quoted token" {
						value = `ab"PRIVATE-token`
					} else if reflected == "query" {
						value = r.URL.Query().Get("key")
					}
					_, _ = fmt.Fprintf(buffer, "invalid %s\r\n\r\n", value)
					_ = buffer.Flush()
				}))
				defer srv.Close()
				endpoint := srv.URL + "/api/webhooks/123/PRIVATE-token?key=PRIVATE-query"
				if reflected == "quoted token" {
					endpoint = srv.URL + "/api/webhooks/123/ab%22PRIVATE-token"
				}
				var sender Sender = NewDiscordClient(endpoint)
				if kind == "ntfy" {
					sender = NewNtfyClient(endpoint, "PRIVATE-bearer")
				}
				err := sender.Send(context.Background(), Notification{Title: "Test", Body: "Body"})
				if err == nil || strings.Contains(err.Error(), "PRIVATE") || !strings.Contains(err.Error(), "malformed HTTP") {
					t.Fatalf("unsafe or missing protocol error: %v", err)
				}
			})
		}
	}
}
