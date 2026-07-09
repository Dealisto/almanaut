package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscordClientPostsEmbed(t *testing.T) {
	var gotContentType string
	var payload discordPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := NewDiscordClient(srv.URL).Send(context.Background(), Notification{
		Title: "Certificate expiring", Body: "a.com expires in 5 days (2026-07-06)", Tags: "warning",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	if len(payload.Embeds) != 1 {
		t.Fatalf("want 1 embed, got %d", len(payload.Embeds))
	}
	e := payload.Embeds[0]
	if e.Title != "Certificate expiring" {
		t.Errorf("Title = %q", e.Title)
	}
	if e.Description != "a.com expires in 5 days (2026-07-06)" {
		t.Errorf("Description = %q", e.Description)
	}
	if e.Color != discordColourWarning {
		t.Errorf("Color = %d, want warning colour %d", e.Color, discordColourWarning)
	}
}

func TestDiscordClientDefaultColourWithoutWarningTag(t *testing.T) {
	var payload discordPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := NewDiscordClient(srv.URL).Send(context.Background(), Notification{Title: "x", Body: "y"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(payload.Embeds) != 1 || payload.Embeds[0].Color != discordColourDefault {
		t.Errorf("want default colour %d, got %+v", discordColourDefault, payload.Embeds)
	}
}

func TestDiscordClientErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad webhook", http.StatusNotFound)
	}))
	defer srv.Close()

	if err := NewDiscordClient(srv.URL).Send(context.Background(), Notification{Body: "x"}); err == nil {
		t.Fatal("expected error on 404")
	}
}
