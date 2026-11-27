package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

// independentCSVStager preserves the old one-Stage-per-row API. As an unknown
// stager it deliberately opts out of ForTransaction's safe durable fast path.
type independentCSVStager struct{ webhook.Dispatcher }

func (d independentCSVStager) Stage(tx *sql.Tx, events ...webhook.Event) error {
	return webhook.Stage(tx, d.Dispatcher, events...)
}

// BenchmarkCSVWebhookStaging includes CSV parsing, inventory and history writes,
// and one webhook delivery per row. Rollback after the last event keeps the
// inventory/backlog identical between samples without exposing test deliveries.
func BenchmarkCSVWebhookStaging(b *testing.B) {
	for _, rows := range []int{1, 100} {
		b.Run(fmt.Sprintf("backlog=9000/rows=%d", rows), func(b *testing.B) {
			db, err := store.Open(filepath.Join(b.TempDir(), "csv.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			if err := store.Migrate(db, ""); err != nil {
				b.Fatal(err)
			}
			endpoint, err := store.NewWebhookRepo(db).Create(domain.Webhook{URL: "https://receiver.test", Secret: "secret", Enabled: true})
			if err != nil {
				b.Fatal(err)
			}
			if err := store.WithTx(db, func(tx *sql.Tx) error {
				stmt, err := tx.Prepare(`INSERT INTO webhook_outbox(delivery_id,webhook_id,url,body,signature,due_at) VALUES(?,?,?,zeroblob(256),'signed',?)`)
				if err != nil {
					return err
				}
				defer stmt.Close()
				for i := range 9000 {
					if _, err := stmt.Exec(fmt.Sprintf("existing-%d", i), endpoint, "https://receiver.test", time.Now().Add(24*time.Hour).UnixMilli()); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			queue := webhook.NewDurableQueue(db, webhook.Options{Workers: 1, Logger: log.New(io.Discard, "", 0)})
			defer func() {
				if err := queue.Shutdown(context.Background()); err != nil {
					b.Error(err)
				}
			}()
			rollback := errors.New("benchmark rollback")
			rs := resource[domain.Host]{
				sing: "host", repo: store.NewHostRepo(db), parse: parseHost,
				label:   func(h domain.Host) string { return h.Name },
				id:      func(h domain.Host) int64 { return h.ID },
				setID:   func(h *domain.Host, id int64) { h.ID = id },
				newItem: domain.Host{Type: "physical"},
				captureEffects: func(_ *sql.Tx, _, next *domain.Host) (mutationEffects, error) {
					if next.Name != fmt.Sprintf("row-%d", rows-1) {
						return nil, nil
					}
					return func(handlerDeps, string, *[]webhook.Event) error { return rollback }, nil
				},
			}
			var document strings.Builder
			document.WriteString("name,type\n")
			for i := range rows {
				fmt.Fprintf(&document, "row-%d,physical\n", i)
			}
			for _, scoped := range []bool{false, true} {
				b.Run(map[bool]string{false: "independent", true: "scoped"}[scoped], func(b *testing.B) {
					deps := handlerDeps{db: db, changelog: store.NewChangelogRepo(db), webhooks: queue}
					if !scoped {
						deps.webhooks = independentCSVStager{queue}
					}
					b.ReportAllocs()
					for b.Loop() {
						_, _, rowErrs, err := rs.importCSV(deps, strings.NewReader(document.String()), "benchmark")
						if len(rowErrs) != 0 || !errors.Is(err, rollback) {
							b.Fatalf("row errors=%v err=%v", rowErrs, err)
						}
					}
				})
			}
		})
	}
}
