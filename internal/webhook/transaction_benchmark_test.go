package webhook

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// BenchmarkTransactionStagingRows uses the CSV mutation call shape: one Stage
// per row, one endpoint, one enclosing transaction. It compares the existing
// independent Stage API with its explicit transaction scope, not one artificial
// Stage(events...) call. Inventory parsing/history are outside this benchmark.
func BenchmarkTransactionStagingRows(b *testing.B) {
	for _, tc := range []struct{ backlog, rows int }{{0, 1}, {9000, 1}, {9000, 100}} {
		b.Run(fmt.Sprintf("backlog=%d/rows=%d", tc.backlog, tc.rows), func(b *testing.B) {
			db, err := store.Open(filepath.Join(b.TempDir(), "outbox.db"))
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
				stmt, err := tx.Prepare(`INSERT INTO webhook_outbox(delivery_id,webhook_id,url,body,signature,due_at) VALUES(?,?,?,zeroblob(256),'signed',0)`)
				if err != nil {
					return err
				}
				defer stmt.Close()
				for i := range tc.backlog {
					if _, err := stmt.Exec(fmt.Sprintf("existing-%d", i), endpoint, "https://receiver.test"); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			q := &DurableQueue{db: db, capacity: DefaultOutboxCapacity}
			rollback := errors.New("benchmark rollback")
			for _, scoped := range []bool{false, true} {
				b.Run(map[bool]string{false: "independent", true: "scoped"}[scoped], func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						err := store.WithTx(db, func(tx *sql.Tx) error {
							var dispatcher Dispatcher = q
							if scoped {
								dispatcher = ForTransaction(tx, dispatcher)
							}
							for i := range tc.rows {
								if err := Stage(tx, dispatcher, Event{Type: "host", ID: int64(i + 1), Action: ActionCreated}); err != nil {
									return err
								}
							}
							return rollback // each iteration retains the same backlog
						})
						if !errors.Is(err, rollback) {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
