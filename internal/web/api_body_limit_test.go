package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestAPIBodyLimitsReturnJSON413WithoutMutation(t *testing.T) {
	for _, path := range []string{"/api/hosts", "/api/hosts/1", "/api/agent/report"} {
		t.Run(path, func(t *testing.T) {
			h, db := newTestServerDB(t)
			var token string
			if path == "/api/agent/report" {
				h, db, token = agentServer(t, domain.ScopeAgent)
			}
			method := http.MethodPost
			if path == "/api/hosts/1" {
				method = http.MethodPut
			}
			limited := limitBodyBytes(64, h)
			for _, unknownLength := range []bool{false, true} {
				req := httptest.NewRequest(method, path, strings.NewReader(strings.Repeat("x", 128)))
				if unknownLength {
					req.ContentLength = -1
				}
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				rec := httptest.NewRecorder()
				limited.ServeHTTP(rec, req)
				var body map[string]string
				if rec.Code != http.StatusRequestEntityTooLarge || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["error"] != "request body too large" {
					t.Fatalf("unknownLength=%v: status=%d content-type=%q body=%s", unknownLength, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
				}
			}
			hosts, err := store.NewHostRepo(db).List()
			if err != nil || len(hosts) != 0 {
				t.Fatalf("oversized request changed inventory: hosts=%v err=%v", hosts, err)
			}
		})
	}
}
