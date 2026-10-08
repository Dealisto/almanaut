package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
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
