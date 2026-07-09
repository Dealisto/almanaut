package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewEventDeleteOmitsData(t *testing.T) {
	e, err := NewEvent("host", 7, ActionDeleted, "alice", "2026-07-09T00:00:00Z", nil)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	if e.Data != nil {
		t.Errorf("delete event Data = %q, want nil", e.Data)
	}
	body, _, err := buildBody(e, "deadbeef", "secret")
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}
	if strings.Contains(string(body), `"data"`) {
		t.Errorf("delete payload should omit data: %s", body)
	}
}

func TestBuildBodySignatureAndFields(t *testing.T) {
	type host struct {
		Name string `json:"name"`
	}
	e, err := NewEvent("host", 42, ActionCreated, "bob", "2026-07-09T12:00:00Z", host{Name: "box"})
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	body, sig, err := buildBody(e, "abc123", "topsecret")
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}

	// Signature is HMAC-SHA256 of the exact body bytes.
	mac := hmac.New(sha256.New, []byte("topsecret"))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig != want {
		t.Errorf("signature = %q, want %q", sig, want)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got["event"] != "created" || got["entity_type"] != "host" || got["actor"] != "bob" {
		t.Errorf("payload fields wrong: %v", got)
	}
	if got["delivery_id"] != "abc123" {
		t.Errorf("delivery_id = %v", got["delivery_id"])
	}
	data, ok := got["data"].(map[string]any)
	if !ok || data["name"] != "box" {
		t.Errorf("data = %v", got["data"])
	}
}
