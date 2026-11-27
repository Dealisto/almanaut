package webhook

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func BenchmarkDurableStageBacklog(b *testing.B) {
	for _, tc := range []struct{ backlog, endpoints int }{{0, 1}, {1000, 10}, {9000, 25}} {
		b.Run(fmt.Sprintf("backlog=%d/endpoints=%d", tc.backlog, tc.endpoints), func(b *testing.B) {
			db, err := store.Open(filepath.Join(b.TempDir(), "outbox.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			if err := store.Migrate(db, ""); err != nil {
				b.Fatal(err)
			}
			var endpoint int64
			for i := range tc.endpoints {
				id, err := store.NewWebhookRepo(db).Create(domain.Webhook{URL: fmt.Sprintf("http://receiver-%d.test", i), Secret: "secret", Enabled: true})
				if err != nil {
					b.Fatal(err)
				}
				if i == 0 {
					endpoint = id
				}
			}
			// Seed directly in one transaction outside the timer, so setup does
			// not measure the very repeated-admission work under investigation.
			if err := store.WithTx(db, func(tx *sql.Tx) error {
				stmt, err := tx.Prepare(`INSERT INTO webhook_outbox(delivery_id,webhook_id,url,body,signature,due_at) VALUES(?,?,?,zeroblob(256),'signed',0)`)
				if err != nil {
					return err
				}
				defer stmt.Close()
				for i := range tc.backlog {
					if _, err := stmt.Exec(fmt.Sprintf("existing-%d", i), endpoint, "http://receiver.test"); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			q := &DurableQueue{db: db, repo: store.NewWebhookOutboxRepo(db), capacity: DefaultOutboxCapacity}
			// Production mutations stage one event at a time. Measure that call
			// shape with endpoint fan-out, not a synthetic multi-event import.
			event := Event{Type: "host", ID: 1, Action: ActionCreated}
			b.ReportAllocs()
			for b.Loop() {
				tx, err := db.Begin()
				if err != nil {
					b.Fatal(err)
				}
				err = q.Stage(tx, event)
				rbErr := tx.Rollback() // each sample starts with the same backlog
				if err != nil || rbErr != nil {
					b.Fatalf("stage=%v rollback=%v", err, rbErr)
				}
			}
		})
	}
}
