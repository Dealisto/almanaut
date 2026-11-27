package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func TestDiscordClientBoundsEmbedText(t *testing.T) {
	for _, glyph := range []string{"a", "é", "界", "🛰"} {
		t.Run(glyph, func(t *testing.T) {
			var payload discordPayload
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode payload: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if len(payload.Embeds) != 1 ||
					len(utf16.Encode([]rune(payload.Embeds[0].Title))) > 256 ||
					len(utf16.Encode([]rune(payload.Embeds[0].Description))) > 4096 {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()
			n := Notification{Title: strings.Repeat(glyph, 300), Body: strings.Repeat(glyph, 5000)}
			if err := NewDiscordClient(srv.URL).Send(context.Background(), n); err != nil {
				t.Fatalf("long notification was rejected: %v", err)
			}
			for _, text := range []string{payload.Embeds[0].Title, payload.Embeds[0].Description} {
				if !utf8.ValidString(text) || !strings.HasSuffix(text, "…") || strings.ContainsRune(text, utf8.RuneError) {
					t.Fatalf("truncated text must preserve Unicode and indicate omission: %q", text)
				}
			}
		})
	}
}

func TestDiscordClientPreservesTextAtLimit(t *testing.T) {
	for _, glyph := range []string{"a", "é", "🛰"} {
		t.Run(glyph, func(t *testing.T) {
			width := len(utf16.Encode([]rune(glyph)))
			n := Notification{Title: strings.Repeat(glyph, 256/width), Body: strings.Repeat(glyph, 4096/width)}
			var payload discordPayload
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode payload: %v", err)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()
			if err := NewDiscordClient(srv.URL).Send(context.Background(), n); err != nil {
				t.Fatal(err)
			}
			if len(payload.Embeds) != 1 || payload.Embeds[0].Title != n.Title || payload.Embeds[0].Description != n.Body {
				t.Fatal("text at the limit was changed")
			}
		})
	}
}
