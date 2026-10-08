package redact

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://discord.com/api/webhooks/123/SECRET": "https://discord.com/…",
		"https://ntfy.sh/my-secret-topic":             "https://ntfy.sh/…",
		"https://hooks.example.com/in?token=SECRET":   "https://hooks.example.com/…",
		"https://user:pass@example.com/":              "https://example.com",
		"http://10.0.0.5:8080":                        "http://10.0.0.5:8080",
		"::not a url":                                 "<redacted url>",
		"no-scheme-SECRET":                            "<redacted url>",
	} {
		if got := URL(raw); got != want {
			t.Errorf("URL(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestErrorHidesSecretFromTransportFailure reproduces the leak: a failed
// request's error names the full URL, token included.
func TestErrorHidesSecretFromTransportFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:1/api/webhooks/123/SECRETTOKEN", nil)
	_, err := http.DefaultClient.Do(req)
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if !strings.Contains(err.Error(), "SECRETTOKEN") {
		t.Fatalf("premise: raw error %q should contain the token", err)
	}

	got := fmt.Errorf("post discord: %w", Error(err))
	if strings.Contains(got.Error(), "SECRETTOKEN") {
		t.Fatalf("redacted error still leaks the token: %v", got)
	}
	if !strings.Contains(got.Error(), "127.0.0.1:1") {
		t.Errorf("redacted error lost the host: %v", got)
	}
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Errorf("redaction broke errors.Is on the cause: %v", got)
	}
}

func TestErrorWithoutURLErrorIsUnchanged(t *testing.T) {
	err := errors.New("discord status 500: boom")
	if got := Error(err); got != err {
		t.Fatalf("Error changed an error with no *url.Error: %v", got)
	}
}
