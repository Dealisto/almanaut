package redact

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestErrorWithSecretsPreservesCauseAndTimeout(t *testing.T) {
	const secret = "private-token"
	cause := &url.Error{Op: "Post", URL: "https://example.test/private-path", Err: context.DeadlineExceeded}
	err := ErrorWithSecrets(cause, secret, "")
	if !errors.Is(err, context.DeadlineExceeded) || !err.(interface{ Timeout() bool }).Timeout() {
		t.Fatalf("lost deadline identity or timeout: %v", err)
	}
	if strings.Contains(err.Error(), "private-path") {
		t.Fatalf("URL path leaked: %v", err)
	}
	reflected := fmt.Errorf("body read failed: %s: %w", secret, context.Canceled)
	err = ErrorWithSecrets(reflected, secret)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), secret) {
		t.Fatalf("lost cancellation or exposed reflected token: %v", err)
	}
	if ErrorWithSecrets(nil, secret) != nil {
		t.Fatal("nil error became a failure")
	}
}

func TestErrorWithURLRedactsReflectedComponents(t *testing.T) {
	endpoint := "https://private-user:private-password@example.test/hooks/private%2Ftoken?key=private+query#private-fragment"
	for _, reflected := range []string{
		endpoint, "/hooks/private/token", "private%2Ftoken", "private/token",
		"private query", "private+query", "private-user", "private-password", "private-fragment", "private-bearer",
	} {
		cause := fmt.Errorf("protocol failure %s: %w", reflected, context.DeadlineExceeded)
		err := ErrorWithURL(cause, endpoint, "private-bearer")
		if strings.Contains(err.Error(), "private") || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("reflected %q: leaked credentials or lost cause: %v", reflected, err)
		}
	}
	cause := &url.Error{Op: "Post", URL: endpoint, Err: context.DeadlineExceeded}
	err := ErrorWithURL(cause, endpoint)
	if !err.(interface{ Timeout() bool }).Timeout() || !strings.Contains(err.Error(), "example.test") {
		t.Fatalf("lost timeout behavior or diagnostic host: %v", err)
	}
	if ErrorWithURL(nil, endpoint) != nil {
		t.Fatal("nil error became a failure")
	}
}

func TestErrorWithURLPrefersCompleteCredentials(t *testing.T) {
	endpoint := "https://example.test/api/api-PRIVATE-token?short=key&long=key-PRIVATE-value"
	for _, reflected := range []string{"api-PRIVATE-token", "key-PRIVATE-value"} {
		err := ErrorWithURL(errors.New("invalid "+reflected), endpoint)
		if got := err.Error(); got != "invalid [redacted]" {
			t.Errorf("partially redacted credential: %q", got)
		}
	}
	secrets := []string{"key", "key-PRIVATE-value"}
	if got := ErrorWithSecrets(errors.New("key-PRIVATE-value"), secrets...).Error(); got != "[redacted]" {
		t.Errorf("partially redacted explicit secret: %q", got)
	}
	if secrets[0] != "key" || secrets[1] != "key-PRIVATE-value" {
		t.Fatal("redaction mutated the caller's secret list")
	}
}
