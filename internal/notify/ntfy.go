package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
		url:    url,
		token:  token,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *ntfyClient) Send(ctx context.Context, n Notification) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, strings.NewReader(n.Body))
	if err != nil {
		return fmt.Errorf("build ntfy request: %w", err)
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
		return fmt.Errorf("post ntfy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ntfy status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
