package webhook

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
)

func TestTransactionStagingRollsBackAtCapacity(t *testing.T) {
	for _, bytes := range []bool{false, true} {
		t.Run(map[bool]string{false: "count", true: "bytes"}[bytes], func(t *testing.T) {
			db, _ := durableDB(t)
			addDurableEndpoint(t, db, "http://receiver.test", "host")
			q := &DurableQueue{db: db, capacity: 1}
			event := Event{Type: "host", ID: 1, Action: ActionCreated}
			wantRemaining := 0
			if bytes {
				q.capacity = 10
				// Leave enough bytes for one event, but not the second.
				if _, err := db.Exec(`INSERT INTO webhook_outbox(delivery_id,webhook_id,url,body,signature,due_at) VALUES('seed',1,'x',zeroblob(?),'',0)`, MaxOutboxBytes-400); err != nil {
					t.Fatal(err)
				}
				wantRemaining = 1
			}
			err := store.WithTx(db, func(tx *sql.Tx) error {
				d := ForTransaction(tx, q)
				if _, err := tx.Exec(`INSERT INTO hosts(name,type) VALUES('rollback','physical')`); err != nil {
					return err
				}
				if err := Stage(tx, d, event); err != nil {
					t.Fatalf("first row rejected: %v", err)
				}
				return Stage(tx, d, event)
			})
			if !errors.Is(err, store.ErrWebhookBacklog) {
				t.Fatalf("second row exceeded budget: %v", err)
			}
			var hosts int
			if err := db.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&hosts); err != nil || hosts != 0 {
				t.Fatalf("inventory rollback: count=%d err=%v", hosts, err)
			}
			if count, err := store.NewWebhookOutboxRepo(db).PendingCount(); err != nil || count != wantRemaining {
				t.Fatalf("outbox rollback: count=%d err=%v", count, err)
			}
		})
	}
}

func TestTransactionStagingRejectsForeignTransactionAndKeepsFailureBudget(t *testing.T) {
	db, _ := durableDB(t)
	otherDB, _ := durableDB(t)
	addDurableEndpoint(t, db, "http://receiver.test")
	q := &DurableQueue{db: db, capacity: 2}
	event := Event{Type: "host", ID: 1, Action: ActionCreated}
	if err := store.WithTx(db, func(tx *sql.Tx) error {
		d := ForTransaction(tx, q)
		if err := store.WithTx(otherDB, func(other *sql.Tx) error {
			if err := Stage(other, d, event); err == nil {
				t.Fatal("scope accepted a foreign transaction")
			}
			return nil
		}); err != nil {
			return err
		}
		invalid := event
		invalid.Data = json.RawMessage(`{`)
		if err := Stage(tx, d, invalid); err == nil {
			t.Fatal("invalid payload accepted")
		}
		if _, err := tx.Exec(`CREATE TEMP TRIGGER reject_scope_delivery BEFORE INSERT ON webhook_outbox WHEN (SELECT COUNT(*) FROM webhook_outbox)=0 BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
			return err
		}
		if err := Stage(tx, d, event); err == nil {
			t.Fatal("injected write failure ignored")
		}
		if _, err := tx.Exec(`DROP TRIGGER reject_scope_delivery`); err != nil {
			return err
		}
		// Failed serialization/INSERT must not spend the two remaining slots.
		if err := Stage(tx, d, event); err != nil {
			return err
		}
		return Stage(tx, d, event)
	}); err != nil {
		t.Fatal(err)
	}
	if count, err := store.NewWebhookOutboxRepo(db).PendingCount(); err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

type legacyStager struct {
	recordingDispatcher
	staged int
}

func (d *legacyStager) Stage(_ *sql.Tx, events ...Event) error {
	d.staged += len(events)
	return nil
}

func TestTransactionStagingDispatcherCompatibility(t *testing.T) {
	db, _ := durableDB(t)
	addDurableEndpoint(t, db, "http://receiver.test")
	q := &DurableQueue{db: db, capacity: 10}
	legacy, staged := &recordingDispatcher{}, &legacyStager{}
	event := Event{Type: "host", ID: 1, Action: ActionCreated}
	var scoped Dispatcher
	if err := store.WithTx(db, func(tx *sql.Tx) error {
		nested := Multi{Noop{}, q}
		scoped = ForTransaction(tx, Multi{legacy, &nested})
		for range 2 {
			if err := Stage(tx, scoped, event); err != nil {
				return err
			}
		}
		if len(legacy.got) != 0 {
			t.Fatal("legacy dispatch ran before commit")
		}
		// Unknown stagers must still be called normally, alongside the queue.
		if err := Stage(tx, ForTransaction(tx, Multi{q, staged}), event); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	scoped.Dispatch(event, event)
	if len(legacy.got) != 2 || staged.staged != 1 {
		t.Fatalf("legacy dispatch=%d stage=%d", len(legacy.got), staged.staged)
	}
	if ForTransaction(nil, legacy) != legacy || ForTransaction(nil, nil) != nil {
		t.Fatal("legacy-only dispatcher changed")
	}
	q.capacity = 4 // three rows already queued; duplicate queues share one slot
	err := store.WithTx(db, func(tx *sql.Tx) error {
		return Stage(tx, ForTransaction(tx, Multi{q, Multi{q}}), event)
	})
	if !errors.Is(err, store.ErrWebhookBacklog) {
		t.Fatalf("multiple queues bypassed shared capacity: %v", err)
	}
}

func TestTransactionStagingConcurrentWritersRespectCapacity(t *testing.T) {
	db, path := durableDB(t)
	other, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	addDurableEndpoint(t, db, "http://receiver.test")
	start := make(chan struct{})
	results := make(chan error, 2)
	queue := &DurableQueue{db: db, capacity: 3}
	var wg sync.WaitGroup
	for _, handle := range []*sql.DB{db, other} {
		wg.Go(func() {
			<-start
			results <- store.WithTx(handle, func(tx *sql.Tx) error {
				d := ForTransaction(tx, queue)
				for range 2 {
					if err := Stage(tx, d, Event{Type: "host", ID: 1, Action: ActionCreated}); err != nil {
						return err
					}
				}
				return nil
			})
		})
	}
	close(start)
	wg.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, store.ErrWebhookBacklog) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	count, err := store.NewWebhookOutboxRepo(db).PendingCount()
	if accepted != 1 || rejected != 1 || count != 2 || err != nil {
		t.Fatalf("accepted=%d rejected=%d count=%d err=%v", accepted, rejected, count, err)
	}
}

func TestTransactionStagingRefreshesAfterRollback(t *testing.T) {
	db, _ := durableDB(t)
	addDurableEndpoint(t, db, "http://old.test")
	queue := &DurableQueue{db: db, capacity: 1}
	event := Event{Type: "host", ID: 1, Action: ActionCreated}
	rollback := errors.New("cancel inventory write")
	if err := store.WithTx(db, func(tx *sql.Tx) error {
		if err := Stage(tx, ForTransaction(tx, queue), event); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE webhooks SET enabled=0`); err != nil {
		t.Fatal(err)
	}
	addDurableEndpoint(t, db, "http://new.test")
	if err := store.WithTx(db, func(tx *sql.Tx) error {
		return Stage(tx, ForTransaction(tx, queue), event)
	}); err != nil {
		t.Fatalf("fresh transaction reused a rolled-back budget: %v", err)
	}
	var url string
	if err := db.QueryRow(`SELECT url FROM webhook_outbox`).Scan(&url); err != nil || url != "http://new.test" {
		t.Fatalf("fresh transaction reused old endpoints: url=%q err=%v", url, err)
	}
}
