package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/redact"
)

// Discord embed colours (decimal RGB, as the webhook API expects).
const (
	discordColourWarning = 0xE67E22 // orange — an item with the "warning" tag
	discordColourDefault = 0x5865F2 // blurple — anything else
)

// discordClient posts notifications to a Discord incoming-webhook URL as a
// single embed. The webhook URL embeds a secret token, so it is treated as a
// secret (the `_FILE` convention in config).
type discordClient struct {
	url    string
	client *http.Client
}

// NewDiscordClient returns a Sender posting to the given Discord incoming-webhook
// URL.
func NewDiscordClient(url string) Sender {
	return &discordClient{
		url: url,
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

// discordPayload is the subset of Discord's webhook body we send: one embed.
type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Color       int    `json:"color"`
}

func (c *discordClient) Send(ctx context.Context, n Notification) error {
	colour := discordColourDefault
	if hasTag(n.Tags, "warning") {
		colour = discordColourWarning
	}
	body, err := json.Marshal(discordPayload{Embeds: []discordEmbed{{
		Title:       discordText(n.Title, 256),
		Description: discordText(n.Body, 4096),
		Color:       colour,
	}}})
	if err != nil {
		return fmt.Errorf("marshal discord payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build discord request: %w", redact.ErrorWithURL(err, c.url))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("post discord: %w", redact.ErrorWithURL(err, c.url))
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Gateways may echo the webhook URL or its token. Keep response
		// bodies out of errors that are logged or shown on the tasks page.
		return fmt.Errorf("discord status %d", resp.StatusCode)
	}
	return nil
}

// discordText bounds an embed field without splitting Unicode characters.
// Count supplementary characters as two UTF-16 units conservatively so clients
// that measure string lengths that way also stay within Discord's limits.
// A single title and description remain below the combined 6000-character cap.
func discordText(text string, limit int) string {
	used, end := 0, 0
	for i, r := range text {
		if used < limit {
			end = i // leave one unit for the omission marker
		}
		used++
		if r > 0xffff {
			used++
		}
		if used > limit {
			return text[:end] + "…"
		}
	}
	return text
}

// hasTag reports whether want appears in a comma-separated tag list.
func hasTag(tags, want string) bool {
	for t := range strings.SplitSeq(tags, ",") {
		if strings.TrimSpace(t) == want {
			return true
		}
	}
	return false
}
