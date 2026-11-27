package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
)

func TestSendRedactsReflectedEndpointSegments(t *testing.T) {
	for _, protocolError := range []bool{false, true} {
		for _, segment := range []string{"PRIVATE-path", `ab"PRIVATE-path`} {
			t.Run(fmt.Sprintf("protocol=%t/%s", protocolError, segment), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if protocolError {
						conn, buffer, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						defer conn.Close()
						_, _ = fmt.Fprintf(buffer, "invalid %s\r\n\r\n", segment)
						_ = buffer.Flush()
						return
					}
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid endpoint " + segment})
				}))
				defer srv.Close()
				endpoint := srv.URL + "/gateway/" + strings.ReplaceAll(segment, `"`, "%22")
				_, err := Send(context.Background(), srv.Client(), endpoint, "bearer-token", agentapi.Report{})
				if err == nil || strings.Contains(err.Error(), "PRIVATE") {
					t.Fatalf("unsafe or missing error: %v", err)
				}
			})
		}
	}
}

func TestServerMessageRedactsOverlappingSecrets(t *testing.T) {
	message, err := serverMessage(strings.NewReader(`{"error":"invalid /api/PRIVATE-token"}`), "/api", "/api/PRIVATE-token")
	if err != nil || strings.Contains(message, "PRIVATE") || !strings.Contains(message, "invalid") {
		t.Fatalf("overlapping secret exposed: %s, %v", message, err)
	}
}
