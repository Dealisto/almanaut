package kuma

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginErrorsDoNotExposeCredentials(t *testing.T) {
	for _, scenario := range []string{"refused", "long secret", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFakeKuma(t)
			password := "PRIVATE-password"
			if scenario == "long secret" {
				password += strings.Repeat("x", 250)
			}
			payload := map[string]any{"ok": false, "msg": "Rejected password " + password}
			if scenario == "malformed" {
				payload["ok"] = password
			}
			ack, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			f.loginAckOverride = string(ack)
			session, err := NewClient(f.url(), "admin", password, false).Connect(testCtx(t))
			if session != nil {
				session.Close()
			}
			if err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("unsafe or missing login error: %v", err)
			}
		})
	}
}

func TestDialErrorsRedactEndpointAndPreserveCancellation(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient(srv.URL+"/PRIVATE-path", "admin", "PRIVATE-password", false).Connect(ctx)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("lost cancellation or exposed endpoint: %v", err)
	}
}

func TestLoginRefusalRedactsLongEndpointBeforeTruncating(t *testing.T) {
	secret := "PRIVATE-path-" + strings.Repeat("x", 250)
	payload, err := json.Marshal(map[string]any{"ok": false, "msg": "Rejected path " + secret})
	if err != nil {
		t.Fatal(err)
	}
	err = okOrError(payload, "https://example.test/"+secret+"/socket.io/", "admin", "password")
	if err == nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("reflected endpoint was truncated before redaction: %v", err)
	}
}
