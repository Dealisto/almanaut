package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestCanceledAPIBodiesDoNotChangeInventory(t *testing.T) {
	for _, duringRead := range []bool{false, true} {
		for _, operation := range []string{"create", "update", "agent report"} {
			name := operation + "/before read"
			if duringRead {
				name = operation + "/during final read"
			}
			t.Run(name, func(t *testing.T) {
				scope := domain.ScopeReadWrite
				if operation == "agent report" {
					scope = domain.ScopeAgent
				}
				srv, db, token := agentServer(t, scope)
				hosts := store.NewHostRepo(db)
				id, err := hosts.Create(domain.Host{Name: "keep", Type: "vm"})
				if err != nil {
					t.Fatal(err)
				}
				method, path, body := http.MethodPost, "/api/hosts", `{"name":"new","type":"vm"}`
				if operation == "update" {
					method, path = http.MethodPut, "/api/hosts/"+itoa(id)
				} else if operation == "agent report" {
					path, body = "/api/agent/report", `{"schema_version":1,"agent_id":"new-agent","hostname":"new-host","virt_kind":"vm"}`
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				reader := &cancelOnFinalRead{body: body, cancel: cancel}
				if !duringRead {
					cancel()
				}
				req := httptest.NewRequest(method, path, reader).WithContext(ctx)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				if rec.Code != http.StatusBadRequest || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
					t.Errorf("canceled API request status=%d body=%s", rec.Code, rec.Body)
				}
				if !duringRead && reader.reads != 0 {
					t.Errorf("read body %d times after request cancellation", reader.reads)
				}
				remaining, err := hosts.List()
				if err != nil || len(remaining) != 1 || remaining[0].ID != id || remaining[0].Name != "keep" {
					t.Errorf("canceled request changed inventory: %+v %v", remaining, err)
				}
				for _, table := range []string{"host_agents", "agent_reports", "changelog", "webhook_outbox"} {
					var count int
					if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
						t.Errorf("canceled request changed %s: count=%d error=%v", table, count, err)
					}
				}
			})
		}
	}
}

// A buffered body can return valid bytes and EOF even as the request is
// canceled. The caller must inspect cancellation after this successful read.
type cancelOnFinalRead struct {
	body   string
	cancel context.CancelFunc
	reads  int
}

func (r *cancelOnFinalRead) Read(p []byte) (int, error) {
	r.reads++
	n := copy(p, r.body)
	r.body = r.body[n:]
	if r.body == "" {
		r.cancel()
		return n, io.EOF
	}
	return n, nil
}
