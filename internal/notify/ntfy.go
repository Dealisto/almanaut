package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/redact"
)

// Notification is a single message to deliver.
type Notification struct {
	Title string
	Body  string
	Tags  string // comma-separated ntfy tags, e.g. "warning"
}

// Sender delivers notifications. ntfyClient is the production implementation.
type Sender interface {
	Send(ctx context.Context, n Notification) error
}

// ntfyClient posts notifications to an ntfy topic URL.
type ntfyClient struct {
	url    string
	token  string
	client *http.Client
}

// NewNtfyClient returns a Sender posting to the given ntfy topic URL. token is
// an optional bearer for protected topics ("" to omit).
func NewNtfyClient(url, token string) Sender {
	return &ntfyClient{
		url:   url,
		token: token,
		client: &http.Client{
			Timeout: 15 * time.Second,
			// A redirect may replace the notification POST with a bodyless GET.
			// Treat it as a failed delivery, just like outbound entity webhooks.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *ntfyClient) Send(ctx context.Context, n Notification) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, strings.NewReader(n.Body))
	if err != nil {
		return fmt.Errorf("build ntfy request: %w", redact.Error(err))
	}
	if n.Title != "" {
		req.Header.Set("Title", n.Title)
	}
	if n.Tags != "" {
		req.Header.Set("Tags", n.Tags)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("post ntfy: %w", redact.Error(err)) // the topic in the URL is its credential
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Gateways may echo the secret-bearing topic URL or Authorization
		// header. Response bodies must not reach logs or task diagnostics.
		return fmt.Errorf("ntfy status %d", resp.StatusCode)
	}
	return nil
}
