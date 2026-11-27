package webhook

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
)

func TestDurableStageBatchCountsEachMatchingEndpointAtomically(t *testing.T) {
	db, _ := durableDB(t)
	addDurableEndpoint(t, db, "http://first.test", "host")
	addDurableEndpoint(t, db, "http://second.test", "host")
	q := &DurableQueue{db: db, capacity: 1}
	err := store.WithTx(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO hosts(name,type) VALUES('rollback','physical')`); err != nil {
			return err
		}
		return q.Stage(tx, Event{Type: "host", ID: 1, Action: ActionCreated})
	})
	if !errors.Is(err, store.ErrWebhookBacklog) || !emptyOutbox(db) {
		t.Fatalf("multiple endpoints escaped atomic cap: %v", err)
	}
	var hosts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&hosts); err != nil || hosts != 0 {
		t.Fatalf("rejected stage committed inventory: hosts=%d error=%v", hosts, err)
	}
}

func TestDurableStageRefreshesBudgetBetweenCalls(t *testing.T) {
	db, _ := durableDB(t)
	addDurableEndpoint(t, db, "http://first.test", "host")
	addDurableEndpoint(t, db, "http://second.test", "host")
	q := &DurableQueue{db: db, capacity: 2}
	event := Event{Type: "host", ID: 1, Action: ActionCreated}
	if err := store.WithTx(db, func(tx *sql.Tx) error {
		if err := q.Stage(tx, event); err != nil {
			return err
		}
		// Endpoint invalidation deletes one staged row. A later Stage call
		// must read the new budget, never reuse an earlier call's counters.
		if _, err := tx.Exec(`UPDATE webhooks SET enabled=0 WHERE url='http://first.test'`); err != nil {
			return err
		}
		return q.Stage(tx, event)
	}); err != nil {
		t.Fatal(err)
	}
	if count, err := store.NewWebhookOutboxRepo(db).PendingCount(); err != nil || count != 2 {
		t.Fatalf("new stage used stale accounting: count=%d error=%v", count, err)
	}
	if err := store.WithTx(db, func(tx *sql.Tx) error { return q.Stage(tx, event) }); !errors.Is(err, store.ErrWebhookBacklog) {
		t.Fatalf("later transaction bypassed capacity: %v", err)
	}
}

func TestDurableStageBatchRollsBackAfterSerializationFailure(t *testing.T) {
	db, _ := durableDB(t)
	addDurableEndpoint(t, db, "http://receiver.test")
	q := &DurableQueue{db: db, capacity: 10}
	valid := Event{Type: "host", ID: 1, Action: ActionCreated}
	invalid := valid
	invalid.Data = json.RawMessage(`{`)
	err := store.WithTx(db, func(tx *sql.Tx) error { return q.Stage(tx, valid, invalid) })
	if err == nil || !emptyOutbox(db) {
		t.Fatalf("serialization failure kept earlier deliveries: %v", err)
	}
}

func TestDurableStageCountsEarlierCallsInTransaction(t *testing.T) {
	db, _ := durableDB(t)
	addDurableEndpoint(t, db, "http://receiver.test")
	q := &DurableQueue{db: db, capacity: 1}
	event := Event{Type: "host", ID: 1, Action: ActionCreated}
	err := store.WithTx(db, func(tx *sql.Tx) error {
		if err := q.Stage(tx, event); err != nil {
			return err
		}
		return q.Stage(tx, event)
	})
	if !errors.Is(err, store.ErrWebhookBacklog) || !emptyOutbox(db) {
		t.Fatalf("successive staging calls escaped the cap or survived rollback: %v", err)
	}
}
