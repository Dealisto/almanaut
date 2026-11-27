package webhook

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeliveryRedactsMalformedHTTPResponse(t *testing.T) {
	for _, reflection := range []string{"path", "token", "signature"} {
		t.Run(reflection, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, buffer, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				value := r.URL.Path
				if reflection == "token" {
					value = "PRIVATE-token"
				} else if reflection == "signature" {
					value = r.Header.Get("X-Almanaut-Signature")
				}
				_, _ = fmt.Fprintf(buffer, "invalid %s\r\n\r\n", value)
				_ = buffer.Flush()
			}))
			defer srv.Close()
			err := postDelivery(context.Background(), srv.Client(), srv.URL+"/hook/PRIVATE-token", []byte(`{}`), "sha256=PRIVATE-signature", "delivery")
			if err == nil || strings.Contains(err.Error(), "PRIVATE") || !strings.Contains(err.Error(), "malformed HTTP") {
				t.Fatalf("unsafe or missing protocol error: %v", err)
			}
		})
	}
}
