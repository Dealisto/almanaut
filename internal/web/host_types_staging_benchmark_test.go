package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
	"github.com/go-chi/chi/v5"
)

// Roll back after the last host update so every sample uses the same inventory
// and backlog, and no benchmark event can reach the network.
func BenchmarkHostTypeReassignment(b *testing.B) {
	for _, count := range []int{1, 100} {
		b.Run(fmt.Sprintf("backlog=9000/hosts=%d", count), func(b *testing.B) {
			db, err := store.Open(filepath.Join(b.TempDir(), "types.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			if err := store.Migrate(db, ""); err != nil {
				b.Fatal(err)
			}
			types, hosts := store.NewHostTypeRepo(db), store.NewHostRepo(db)
			if err := types.Create(domain.HostType{Name: "custom", Label: "Custom"}); err != nil {
				b.Fatal(err)
			}
			for i := range count {
				if _, err := hosts.Create(domain.Host{Name: fmt.Sprintf("host-%03d", i), Type: "custom"}); err != nil {
					b.Fatal(err)
				}
			}
			endpoint, err := store.NewWebhookRepo(db).Create(domain.Webhook{URL: "https://receiver.test", Secret: "secret", Enabled: true})
			if err != nil {
				b.Fatal(err)
			}
			if _, err := db.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<9000)
INSERT INTO webhook_outbox(delivery_id,webhook_id,url,body,signature,due_at)
SELECT 'existing-'||i, ?, 'https://receiver.test', zeroblob(256), 'signed', ? FROM n`, endpoint, time.Now().Add(24*time.Hour).UnixMilli()); err != nil {
				b.Fatal(err)
			}
			logger := log.New(io.Discard, "", 0)
			queue := webhook.NewDurableQueue(db, webhook.Options{Workers: 1, Logger: logger})
			defer func() {
				if err := queue.Shutdown(context.Background()); err != nil {
					b.Error(err)
				}
			}()
			rollback := errors.New("benchmark rollback")
			completed := false
			rs := resource[domain.Host]{
				sing: "host", repo: hosts,
				id: func(h domain.Host) int64 { return h.ID }, label: func(h domain.Host) string { return h.Name },
				captureEffects: func(_ *sql.Tx, _, next *domain.Host) (mutationEffects, error) {
					if next.Name == fmt.Sprintf("host-%03d", count-1) {
						return func(handlerDeps, string, *[]webhook.Event) error {
							completed = true
							return rollback
						}, nil
					}
					return nil, nil
				},
			}
			deps := handlerDeps{db: db, changelog: store.NewChangelogRepo(db), customFields: store.NewCustomFieldRepo(db), webhooks: queue}
			router := chi.NewRouter()
			router.Use(injectLogger(logger))
			router.Post("/host-types/{name}/delete", deleteHostType(types, hosts, rs, deps))
			b.ReportAllocs()
			for b.Loop() {
				completed = false
				req := httptest.NewRequest(http.MethodPost, "/host-types/custom/delete", strings.NewReader("replacement=vm"))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusInternalServerError || !completed {
					b.Fatalf("rollback response = %d, reached last host = %t", rec.Code, completed)
				}
			}
			host, err := hosts.Get(1)
			if err != nil || host.Type != "custom" {
				b.Fatalf("reassignment survived rollback: %+v, %v", host, err)
			}
			pending, err := store.NewWebhookOutboxRepo(db).PendingCount()
			if err != nil || pending != 9000 {
				b.Fatalf("backlog after rollback = %d, %v", pending, err)
			}
		})
	}
}
