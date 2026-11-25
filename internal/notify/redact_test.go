package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// deadURL returns the base URL of a server that is already closed, so a
// request to it fails at the transport level (connection refused).
func deadURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	return srv.URL
}

func TestSendHTTPFailuresDoNotLeakEchoedCredentials(t *testing.T) {
	for _, kind := range []string{"ntfy", "discord"} {
		for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
			t.Run(kind+"/"+strconv.Itoa(status), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, r.URL.String()+" "+r.Header.Get("Authorization"), status)
				}))
				defer server.Close()
				var sender Sender
				if kind == "ntfy" {
					sender = NewNtfyClient(server.URL+"/SECRET-topic", "SECRET-bearer")
				} else {
					sender = NewDiscordClient(server.URL + "/api/webhooks/123/SECRET-token")
				}
				err := sender.Send(context.Background(), Notification{Title: "test", Body: "body"})
				if err == nil || !strings.Contains(err.Error(), strconv.Itoa(status)) {
					t.Fatalf("Send error = %v, want status %d", err, status)
				}
				if strings.Contains(err.Error(), "SECRET") {
					t.Fatalf("Send error leaks an echoed credential: %v", err)
				}
			})
		}
	}
}

// TestSendErrorsDoNotLeakURLSecrets: a transport failure must not put the
// Discord webhook token or the ntfy topic in the error, which every job logs.
func TestSendErrorsDoNotLeakURLSecrets(t *testing.T) {
	base := deadURL(t)
	for name, s := range map[string]Sender{
		"discord": NewDiscordClient(base + "/api/webhooks/123/SECRETTOKEN"),
		"ntfy":    NewNtfyClient(base+"/SECRETTOKEN-topic", ""),
	} {
		err := s.Send(context.Background(), Notification{Title: "t", Body: "b"})
		if err == nil {
			t.Fatalf("%s: expected a transport error", name)
		}
		if strings.Contains(err.Error(), "SECRETTOKEN") {
			t.Errorf("%s: error leaks the URL secret: %v", name, err)
		}
		if !strings.Contains(err.Error(), strings.TrimPrefix(base, "http://")) {
			t.Errorf("%s: error should still name the host: %v", name, err)
		}
	}
}
