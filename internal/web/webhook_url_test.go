package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWebhookURLValidation(t *testing.T) {
	for _, tc := range []struct {
		url   string
		valid bool
	}{
		{"http://", false},
		{"https:///hook", false},
		{"https://host:0/hook", false},
		{"https://host:65536/hook", false},
		{"https://host:abc/hook", false},
		{"https://host/hook#secret", false},
		{"http://host/\npath", false},
		{"http://host/hook", true},
		{"https://host:8443/hook?token=secret", true},
		{"http://[::1]:8080/hook", true},
		{"https://user:password@host/hook", true},
		{"https://host/hook%23token", true},
	} {
		t.Run(tc.url, func(t *testing.T) {
			form := url.Values{"url": {tc.url}, "events": {"created"}}
			req := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			_, _, _, err := parseWebhookForm(req)
			if (err == nil) != tc.valid {
				t.Fatalf("valid = %t, want %t: %v", err == nil, tc.valid, err)
			}
		})
	}
}
