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

func TestAPIMutationsRecheckBearerAfterBodyRead(t *testing.T) {
	for _, operation := range []string{"create", "update", "agent"} {
		for _, change := range []string{"revoked", "expired", "demoted", "read-only scope", "other route scope", "storage failure", "unchanged", "renamed"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				scope := domain.ScopeReadWrite
				if operation == "agent" {
					scope = domain.ScopeAgent
				}
				h, db, token := agentServer(t, scope)
				hosts := store.NewHostRepo(db)
				id, err := hosts.Create(domain.Host{Name: "keep", Type: "vm"})
				if err != nil {
					t.Fatal(err)
				}
				method, path, body := http.MethodPost, "/api/hosts", `{"name":"new","type":"vm"}`
				if operation == "update" {
					method, path = http.MethodPut, "/api/hosts/"+itoa(id)
				} else if operation == "agent" {
					path, body = "/api/agent/report", `{"schema_version":1,"agent_id":"new-agent","hostname":"new-host","virt_kind":"vm"}`
				}
				wantStatus, success := http.StatusUnauthorized, false
				switch change {
				case "demoted", "read-only scope", "other route scope":
					wantStatus = http.StatusForbidden
				case "storage failure":
					wantStatus = http.StatusInternalServerError
				case "unchanged", "renamed":
					success, wantStatus = true, http.StatusOK
					if operation == "create" {
						wantStatus = http.StatusCreated
					}
				}
				reader := &beforeBodyRead{Reader: strings.NewReader(body), before: func() {
					query := ""
					switch change {
					case "revoked":
						query = `DELETE FROM api_tokens`
					case "expired":
						query = `UPDATE api_tokens SET expires_at='2000-01-01T00:00:00Z'`
					case "demoted":
						query = `UPDATE users SET role='viewer'`
					case "read-only scope":
						query = `UPDATE api_tokens SET scope='read-only'`
					case "other route scope":
						query = `UPDATE api_tokens SET scope='agent'`
						if operation == "agent" {
							query = `UPDATE api_tokens SET scope='read-write'`
						}
					case "storage failure":
						query = `DROP TABLE api_tokens`
					case "renamed":
						query = `UPDATE users SET username='current-user'`
					}
					if query != "" {
						if _, err := db.Exec(query); err != nil {
							t.Fatal(err)
						}
					}
				}}
				req := httptest.NewRequest(method, path, reader)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != wantStatus || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
					t.Errorf("response status=%d want=%d body=%s", rec.Code, wantStatus, rec.Body)
				}
				if !success {
					var response map[string]string
					if json.Unmarshal(rec.Body.Bytes(), &response) != nil || response["error"] == "" || strings.Contains(rec.Body.String(), token) || strings.Contains(rec.Body.String(), "api_tokens") {
						t.Errorf("missing or unsafe JSON error: %s", rec.Body)
					}
				}
				all, err := hosts.List()
				wantHosts := 1
				if success && operation != "update" {
					wantHosts = 2
				}
				if err != nil || len(all) != wantHosts {
					t.Fatalf("unexpected inventory: hosts=%+v err=%v", all, err)
				}
				kept, err := hosts.Get(id)
				wantName := "keep"
				if success && operation == "update" {
					wantName = "new"
				}
				if err != nil || kept.Name != wantName {
					t.Errorf("unexpected stored host: %+v %v", kept, err)
				}
				for _, table := range []string{"changelog", "host_agents", "agent_reports"} {
					want := 0
					if success && (table == "changelog" || operation == "agent") {
						want = 1
					}
					var count int
					if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != want {
						t.Errorf("%s count=%d want=%d err=%v", table, count, want, err)
					}
				}
				if change == "renamed" && operation != "agent" {
					events, err := store.NewChangelogRepo(db).ListRecent(10)
					if err != nil || len(events) != 1 || events[0].Actor != "current-user" {
						t.Errorf("mutation retained stale actor: events=%+v err=%v", events, err)
					}
				}
				if events, err := store.NewAuthEventRepo(db).List(store.AuthEventFilter{Type: domain.AuthTokenUsed}); err != nil || len(events) != 1 {
					t.Errorf("initial token use audit duplicated or lost: events=%+v err=%v", events, err)
				}
			})
		}
	}
}
