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
		url:    url,
		client: &http.Client{Timeout: 15 * time.Second},
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
		Title:       n.Title,
		Description: n.Body,
		Color:       colour,
	}}})
	if err != nil {
		return fmt.Errorf("marshal discord payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("post discord: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("discord status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
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
