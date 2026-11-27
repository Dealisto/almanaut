package store

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestWebhookOutboxBatchFailedAdmissionsDoNotSpendBudget(t *testing.T) {
	for _, limit := range []string{"count", "bytes"} {
		t.Run(limit, func(t *testing.T) {
			db := newTestDB(t)
			d := seedOutbox(t, db, "first")
			size := int64(len(d.Body) + len(d.URL) + len(d.Signature) + len(d.ID))
			capacity, byteLimit := 3, int64(1<<20)
			if limit == "bytes" {
				capacity, byteLimit = 100, 3*size
			}
			rollback := errors.New("test rollback")
			err := WithTx(db, func(tx *sql.Tx) error {
				batch, err := NewWebhookOutboxBatch(tx, capacity, byteLimit)
				if err != nil {
					return err
				}
				// A duplicate key is a storage failure, not a consumed slot.
				if err := batch.Enqueue(d, time.Now()); err == nil || errors.Is(err, ErrWebhookBacklog) {
					t.Fatalf("duplicate insert error = %v", err)
				}
				oversized := d
				oversized.ID = "large"
				oversized.Body = []byte(strings.Repeat("x", int(byteLimit)+1))
				if err := batch.Enqueue(oversized, time.Now()); !errors.Is(err, ErrWebhookBacklog) {
					t.Fatalf("oversized request error = %v", err)
				}
				for _, id := range []string{"later", "third"} {
					d.ID = id
					if err := batch.Enqueue(d, time.Now()); err != nil {
						t.Fatalf("failed admission spent budget before %s: %v", id, err)
					}
				}
				d.ID = "extra"
				if err := batch.Enqueue(d, time.Now()); !errors.Is(err, ErrWebhookBacklog) {
					t.Fatalf("full batch error = %v", err)
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal(err)
			}
			if n := countRows(t, db, `SELECT COUNT(*) FROM webhook_outbox`); n != 1 {
				t.Fatalf("rolled-back batch changed backlog: %d", n)
			}
		})
	}
}

func TestWebhookOutboxBatchesSerializeAdmissionAcrossWriters(t *testing.T) {
	for _, limit := range []string{"count", "bytes"} {
		t.Run(limit, func(t *testing.T) {
			db := newTestDB(t)
			endpoint, err := NewWebhookRepo(db).Create(domain.Webhook{URL: "http://receiver.test", Secret: "secret", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			d := WebhookDelivery{ID: "a0", WebhookID: endpoint, URL: "http://receiver.test", Body: []byte("payload"), Signature: "signed"}
			size := int64(len(d.Body) + len(d.URL) + len(d.Signature) + len(d.ID))
			capacity, byteLimit := 3, int64(1<<20)
			if limit == "bytes" {
				capacity, byteLimit = 100, 3*size
			}
			// Independent pools model distinct server processes using one database.
			var seq int
			var name, path string
			if err := db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
				t.Fatal(err)
			}
			other, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			start := make(chan struct{})
			results := make(chan error, 2)
			var workers sync.WaitGroup
			for i, writer := range []*sql.DB{db, other} {
				workers.Go(func() {
					<-start
					results <- WithTx(writer, func(tx *sql.Tx) error {
						if _, err := tx.Exec(`INSERT INTO hosts(name,type) VALUES(?, 'physical')`, string(rune('a'+i))); err != nil {
							return err
						}
						batch, err := NewWebhookOutboxBatch(tx, capacity, byteLimit)
						if err != nil {
							return err
						}
						for j := range 2 {
							delivery := d
							delivery.ID = string([]byte{byte('a' + i), byte('0' + j)})
							if err := batch.Enqueue(delivery, time.Now()); err != nil {
								return err
							}
						}
						return nil
					})
				})
			}
			close(start)
			workers.Wait()
			close(results)
			success, rejected := 0, 0
			for err := range results {
				switch {
				case err == nil:
					success++
				case errors.Is(err, ErrWebhookBacklog):
					rejected++
				default:
					t.Fatal(err)
				}
			}
			if success != 1 || rejected != 1 {
				t.Fatalf("successful=%d rejected=%d", success, rejected)
			}
			if n := countRows(t, db, `SELECT COUNT(*) FROM webhook_outbox`); n != 2 {
				t.Fatalf("partial or excessive delivery batch committed: %d", n)
			}
			if n := countRows(t, db, `SELECT COUNT(*) FROM hosts`); n != 1 {
				t.Fatalf("rejected batch committed inventory: %d", n)
			}
		})
	}
}

func TestWebhookOutboxBatchRequiresTransaction(t *testing.T) {
	if _, err := NewWebhookOutboxBatch(nil, 1, 1024); err == nil {
		t.Fatal("batch accepted a missing transaction")
	}
}

func TestWebhookOutboxAdmissionCountsUTF8MetadataBytes(t *testing.T) {
	db := newTestDB(t)
	endpoint, err := NewWebhookRepo(db).Create(domain.Webhook{URL: "http://receiver.test", Secret: "secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	first := WebhookDelivery{ID: "通知", WebhookID: endpoint, URL: "http://receiver.test/é", Body: []byte("payload"), Signature: "signé"}
	next := first
	next.ID = "next"
	size := func(d WebhookDelivery) int64 { return int64(len(d.ID) + len(d.URL) + len(d.Body) + len(d.Signature)) }
	repo := NewWebhookOutboxRepo(db)
	if err := repo.Enqueue(first, time.Now(), 10, 1024); err != nil {
		t.Fatal(err)
	}
	// These admissions read persisted metadata, whose SQL text length is
	// smaller than its UTF-8 storage length. One byte short must reject.
	if err := repo.Enqueue(next, time.Now(), 10, size(first)+size(next)-1); !errors.Is(err, ErrWebhookBacklog) {
		t.Fatalf("UTF-8 metadata bypassed byte limit: %v", err)
	}
	if err := repo.Enqueue(next, time.Now(), 10, size(first)+size(next)); err != nil {
		t.Fatalf("exact byte limit rejected: %v", err)
	}
}
