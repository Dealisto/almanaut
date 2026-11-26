package store

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

func seedOutbox(t *testing.T, db *sql.DB, id string) WebhookDelivery {
	t.Helper()
	endpoint, err := NewWebhookRepo(db).Create(domain.Webhook{URL: "http://receiver.test/hook", Secret: "secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	delivery := WebhookDelivery{ID: id, WebhookID: endpoint, URL: "http://receiver.test/hook", Body: []byte(`{"event":"created"}`), Signature: "signed"}
	if err := NewWebhookOutboxRepo(db).Enqueue(delivery, time.Now(), 10, 1024); err != nil {
		t.Fatal(err)
	}
	return delivery
}

func TestWebhookOutboxClaimsAreExclusiveAndFenceExpiredWorkers(t *testing.T) {
	db := newTestDB(t)
	seedOutbox(t, db, "delivery")
	repo := NewWebhookOutboxRepo(db)
	now := time.Now().Add(time.Second)
	start := make(chan struct{})
	type result struct {
		delivery WebhookDelivery
		err      error
	}
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for _, token := range []string{"worker-one", "worker-two"} {
		workers.Go(func() {
			<-start
			d, err := repo.Claim(now, time.Minute, token)
			results <- result{d, err}
		})
	}
	close(start)
	workers.Wait()
	close(results)
	var original WebhookDelivery
	claimed := 0
	for result := range results {
		if result.err == nil {
			claimed++
			original = result.delivery
		} else if !errors.Is(result.err, ErrNotFound) {
			t.Fatal(result.err)
		}
	}
	if claimed != 1 || original.Attempts != 1 {
		t.Fatalf("claim count=%d delivery=%+v", claimed, original)
	}
	if _, err := repo.Claim(now.Add(30*time.Second), time.Minute, "too-early"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("live lease reclaimed: %v", err)
	}
	replacement, err := repo.Claim(now.Add(time.Minute), time.Minute, "replacement")
	if err != nil || replacement.Attempts != 1 {
		t.Fatalf("expired lease was not recovered: %+v %v", replacement, err)
	}
	if err := repo.Complete(original); err != nil {
		t.Fatal(err)
	}
	if err := repo.Retry(original, now.Add(time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if active, err := repo.Active(replacement); err != nil || !active {
		t.Fatalf("stale worker changed replacement: active=%v err=%v", active, err)
	}
	if err := repo.Retry(replacement, now.Add(2*time.Hour), false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Claim(now.Add(time.Hour), time.Minute, "early-retry"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retry ran before due time: %v", err)
	}
	next, err := repo.Claim(now.Add(2*time.Hour), time.Minute, "confirmed-retry")
	if err != nil || next.Attempts != 2 {
		t.Fatalf("confirmed failure did not consume retry budget: %+v %v", next, err)
	}
}

func TestWebhookOutboxAdmissionRollbackAndByteLimit(t *testing.T) {
	db := newTestDB(t)
	d := seedOutbox(t, db, "first")
	repo := NewWebhookOutboxRepo(db)
	d.ID = "second"
	err := WithTx(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO hosts(name, type) VALUES('must roll back', 'physical')`); err != nil {
			return err
		}
		return repo.WithTx(tx).Enqueue(d, time.Now(), 1, 1024)
	})
	if !errors.Is(err, ErrWebhookBacklog) {
		t.Fatalf("full queue: %v", err)
	}
	if countRows(t, db, `SELECT COUNT(*) FROM hosts`) != 0 {
		t.Fatal("backlog rejection committed the entity")
	}
	if err := repo.Enqueue(d, time.Now(), 10, int64(len(d.Body))); !errors.Is(err, ErrWebhookBacklog) {
		t.Fatalf("payload byte ceiling ignored: %v", err)
	}
	if count, err := repo.PendingCount(); err != nil || count != 1 {
		t.Fatalf("failed admission changed backlog: count=%d err=%v", count, err)
	}
}

func TestWebhookOutboxEndpointChangesCancelDeliveries(t *testing.T) {
	for _, change := range []string{"delete", "disable", "url", "secret", "types", "events"} {
		t.Run(change, func(t *testing.T) {
			db := newTestDB(t)
			d := seedOutbox(t, db, "pending")
			repo := NewWebhookOutboxRepo(db)
			claimed, err := repo.Claim(time.Now().Add(time.Second), time.Minute, "worker")
			if err != nil {
				t.Fatal(err)
			}
			hooks := NewWebhookRepo(db)
			hook, err := hooks.Get(d.WebhookID)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "delete":
				err = hooks.Delete(hook.ID)
			case "disable":
				hook.Enabled = false
			case "url":
				hook.URL = "http://replacement.test"
			case "secret":
				hook.Secret = "replacement"
			case "types":
				hook.EntityTypes = []string{"service"}
			case "events":
				hook.Events = []string{"deleted"}
			}
			if change != "delete" {
				err = hooks.Update(hook)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Retry(claimed, time.Now(), false); err != nil {
				t.Fatal(err)
			}
			if count, err := repo.PendingCount(); err != nil || count != 0 {
				t.Fatalf("endpoint change revived delivery: count=%d err=%v", count, err)
			}
		})
	}
}

func TestWebhookOutboxSurvivesInventoryReplacement(t *testing.T) {
	db := newTestDB(t)
	d := seedOutbox(t, db, "historical-delivery")
	if err := Import(db, Snapshot{Version: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := NewWebhookOutboxRepo(db).Claim(time.Now().Add(time.Second), time.Minute, "after-import")
	if err != nil || got.ID != d.ID || string(got.Body) != string(d.Body) || got.Signature != d.Signature {
		t.Fatalf("import changed immutable historical delivery: %+v %v", got, err)
	}
}
