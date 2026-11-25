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
