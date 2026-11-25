package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNotificationClientsRejectRedirects(t *testing.T) {
	for _, kind := range []string{"ntfy", "discord"} {
		for _, status := range []int{301, 302, 303, 307, 308} {
			t.Run(kind+"/"+strconv.Itoa(status), func(t *testing.T) {
				var forwarded atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					forwarded.Add(1)
					w.WriteHeader(http.StatusOK)
				}))
				defer target.Close()
				source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					location := target.URL + "/private-redirect-secret?token=private-redirect-secret"
					http.Redirect(w, r, location, status)
					_, _ = w.Write([]byte("Continue at " + location))
				}))
				defer source.Close()
				var sender Sender = NewNtfyClient(source.URL, "private-token")
				if kind == "discord" {
					sender = NewDiscordClient(source.URL)
				}
				err := sender.Send(context.Background(), Notification{Title: "Alert", Body: "Inventory details"})
				if err == nil || !strings.Contains(err.Error(), strconv.Itoa(status)) {
					t.Errorf("Send = %v, want redirect status %d", err, status)
				}
				if err != nil && strings.Contains(err.Error(), "private-redirect-secret") {
					t.Errorf("redirect error exposes destination credentials: %v", err)
				}
				if got := forwarded.Load(); got != 0 {
					t.Errorf("forwarded %d requests to redirect target", got)
				}
			})
		}
	}
}
