package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func durableDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "webhooks.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func durableQueue(t *testing.T, db *sql.DB, opts Options) *DurableQueue {
	t.Helper()
	if opts.Logger == nil {
		opts.Logger = log.New(io.Discard, "", 0)
	}
	q := NewDurableQueue(db, opts)
	t.Cleanup(func() { stopDurable(t, q) })
	return q
}

func stopDurable(t *testing.T, q *DurableQueue) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := q.Shutdown(ctx); err != nil {
		t.Errorf("stop durable queue: %v", err)
	}
}

func addDurableEndpoint(t *testing.T, db *sql.DB, url string, types ...string) {
	t.Helper()
	if _, err := store.NewWebhookRepo(db).Create(domain.Webhook{URL: url, Secret: "secret", Enabled: true, EntityTypes: types}); err != nil {
		t.Fatal(err)
	}
}

func stageDurable(t *testing.T, db *sql.DB, dispatcher Dispatcher, events ...Event) {
	t.Helper()
	if err := store.WithTx(db, func(tx *sql.Tx) error { return Stage(tx, dispatcher, events...) }); err != nil {
		t.Fatal(err)
	}
	dispatcher.Dispatch(events...)
}

func awaitDurable(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for durable delivery")
		case <-ticker.C:
		}
	}
}

func emptyOutbox(db *sql.DB) bool {
	n, err := store.NewWebhookOutboxRepo(db).PendingCount()
	return err == nil && n == 0
}

func TestDurableQueueRetriesSameSignedDeliveryAcrossRestart(t *testing.T) {
	db, path := durableDB(t)
	type request struct{ id, signature, body string }
	requests := make(chan request, 2)
	var attempts atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- request{r.Header.Get("X-Almanaut-Delivery"), r.Header.Get("X-Almanaut-Signature"), string(body)}
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(receiver.Close)
	addDurableEndpoint(t, db, receiver.URL)
	q := durableQueue(t, db, Options{Workers: 1, BaseDelay: time.Hour})
	event, err := NewEvent("host", 17, ActionCreated, "alice", "2026-01-01T00:00:00Z", map[string]string{"name": "nas"})
	if err != nil {
		t.Fatal(err)
	}
	stageDurable(t, db, Multi{Noop{}, q}, event)
	awaitDurable(t, func() bool {
		var attempts, lease int64
		err := db.QueryRow(`SELECT attempts, lease_until FROM webhook_outbox`).Scan(&attempts, &lease)
		return err == nil && attempts == 1 && lease == 0
	})
	stopDurable(t, q)
	// Disabled webhooks use Noop: new inventory changes do not enqueue events,
	// and previously accepted deliveries remain available when re-enabled.
	stageDurable(t, db, Noop{}, event)
	if pending, err := store.NewWebhookOutboxRepo(db).PendingCount(); err != nil || pending != 1 {
		t.Fatalf("disabled mode discarded or staged deliveries: pending=%d err=%v", pending, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Open the actual SQLite file again, without carrying any in-memory jobs.
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	if _, err := reopened.Exec(`UPDATE webhook_outbox SET due_at=0`); err != nil {
		t.Fatal(err)
	}
	second := durableQueue(t, reopened, Options{Workers: 1})
	awaitDurable(t, func() bool { return emptyOutbox(reopened) })
	stopDurable(t, second)
	first, retry := <-requests, <-requests
	if first != retry || first.id == "" {
		t.Fatalf("restart changed signed request: first=%+v retry=%+v", first, retry)
	}
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte(first.body))
	if first.signature != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal("persisted signature does not authenticate immutable payload")
	}
}

func TestDurableQueueRecoversUnsentExpiredClaimWithOneAttempt(t *testing.T) {
	db, path := durableDB(t)
	type request struct{ id, signature, body string }
	received := make(chan request, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- request{r.Header.Get("X-Almanaut-Delivery"), r.Header.Get("X-Almanaut-Signature"), string(body)}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	addDurableEndpoint(t, db, receiver.URL)
	hooks, err := store.NewWebhookRepo(db).ListEnabled()
	if err != nil {
		t.Fatal(err)
	}
	id := "stable-interrupted-delivery"
	body, signature, err := buildBody(Event{Type: "host", ID: 1, Action: ActionCreated}, id, "secret")
	if err != nil {
		t.Fatal(err)
	}
	repo := store.NewWebhookOutboxRepo(db)
	past := time.Now().Add(-time.Minute)
	if err := repo.Enqueue(store.WebhookDelivery{ID: id, WebhookID: hooks[0].ID, URL: receiver.URL, Body: body, Signature: signature}, past, 10, 1024); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.Claim(past, time.Second, "crashed-before-http")
	if err != nil || claimed.Attempts != 1 {
		t.Fatalf("initial claim: %+v %v", claimed, err)
	}
	// Simulate process loss immediately after Claim, before any HTTP request.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	q := durableQueue(t, reopened, Options{Workers: 1, MaxAttempts: 1})
	awaitDurable(t, func() bool { return emptyOutbox(reopened) })
	stopDurable(t, q)
	select {
	case got := <-received:
		if got.id != id || got.body != string(body) || got.signature != signature {
			t.Fatalf("expired claim changed immutable delivery: %+v", got)
		}
	default:
		t.Fatal("crash before HTTP exhausted retry budget and lost the delivery")
	}
}

func TestDurableQueueRollbackAndAdmissionAreAtomic(t *testing.T) {
	db, _ := durableDB(t)
	var delivered atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { delivered.Add(1) }))
	t.Cleanup(receiver.Close)
	addDurableEndpoint(t, db, receiver.URL)
	q := durableQueue(t, db, Options{QueueSize: 1, Workers: 1})
	event := Event{Type: "host", ID: 1, Action: ActionCreated}
	rollback := errors.New("abort entity mutation")
	err := store.WithTx(db, func(tx *sql.Tx) error {
		if err := q.Stage(tx, event); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM webhook_outbox`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("staged delivery absent from transaction: %d %v", count, err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) || !emptyOutbox(db) {
		t.Fatalf("rollback retained delivery: %v", err)
	}
	err = store.WithTx(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO hosts(name,type) VALUES('rollback','physical')`); err != nil {
			return err
		}
		return q.Stage(tx, event, event)
	})
	if !errors.Is(err, store.ErrWebhookBacklog) || !emptyOutbox(db) {
		t.Fatalf("overflow silently lost or retained events: %v", err)
	}
	var hosts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&hosts); err != nil || hosts != 0 {
		t.Fatalf("overflow committed entity: %d %v", hosts, err)
	}
	stopDurable(t, q)
	if delivered.Load() != 0 {
		t.Fatal("uncommitted delivery reached receiver")
	}
}

func TestDurableQueueShutdownPreservesInterruptedDelivery(t *testing.T) {
	db, _ := durableDB(t)
	started, canceled := make(chan struct{}), make(chan struct{})
	var attempts atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if attempts.Add(1) == 1 {
			close(started)
			<-r.Context().Done()
			close(canceled)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	addDurableEndpoint(t, db, receiver.URL)
	q := durableQueue(t, db, Options{Workers: 1, MaxAttempts: 1, Timeout: time.Minute})
	stageDurable(t, db, q, Event{Type: "host", ID: 1, Action: ActionCreated})
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery did not start")
	}
	stopDurable(t, q)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel HTTP request")
	}
	var attemptsStored, lease int64
	if err := db.QueryRow(`SELECT attempts,lease_until FROM webhook_outbox`).Scan(&attemptsStored, &lease); err != nil || attemptsStored != 0 || lease != 0 {
		t.Fatalf("shutdown lost delivery or used its retry budget: attempts=%d lease=%d err=%v", attemptsStored, lease, err)
	}
	durableQueue(t, db, Options{Workers: 1, MaxAttempts: 1})
	awaitDurable(t, func() bool { return emptyOutbox(db) })
	if attempts.Load() != 2 {
		t.Fatalf("interrupted delivery was not resumed: %d", attempts.Load())
	}
}

func TestDurableQueueParallelInstancesDoNotDuplicateLiveClaims(t *testing.T) {
	db, _ := durableDB(t)
	var mu sync.Mutex
	seen := make(map[string]int)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Header.Get("X-Almanaut-Delivery")]++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	addDurableEndpoint(t, db, receiver.URL)
	q := durableQueue(t, db, Options{Workers: 2})
	durableQueue(t, db, Options{Workers: 2})
	var events []Event
	for i := range 20 {
		events = append(events, Event{Type: "host", ID: int64(i + 1), Action: ActionCreated})
	}
	stageDurable(t, db, q, events...)
	awaitDurable(t, func() bool { return emptyOutbox(db) })
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != len(events) {
		t.Fatalf("received %d distinct deliveries, want %d", len(seen), len(events))
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("delivery %s sent %d times without an expired lease", id, count)
		}
	}
}

func TestDurableQueueDelayedRetryDoesNotOccupyWorker(t *testing.T) {
	db, _ := durableDB(t)
	var goodCalls atomic.Int32
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	t.Cleanup(bad.Close)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { goodCalls.Add(1) }))
	t.Cleanup(good.Close)
	addDurableEndpoint(t, db, bad.URL, "host")
	addDurableEndpoint(t, db, good.URL, "service")
	q := durableQueue(t, db, Options{Workers: 1, BaseDelay: time.Hour})
	stageDurable(t, db, q, Event{Type: "host", ID: 1, Action: ActionCreated})
	awaitDurable(t, func() bool {
		var attempts, lease int64
		err := db.QueryRow(`SELECT attempts,lease_until FROM webhook_outbox`).Scan(&attempts, &lease)
		return err == nil && attempts == 1 && lease == 0
	})
	stageDurable(t, db, q, Event{Type: "service", ID: 2, Action: ActionCreated})
	awaitDurable(t, func() bool { return goodCalls.Load() == 1 })
}

func TestDurableQueueRejectsRedirectAndStopsAfterRetryBudget(t *testing.T) {
	db, _ := durableDB(t)
	var redirects, attempts atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1) }))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(source.Close)
	addDurableEndpoint(t, db, source.URL)
	client := source.Client()
	q := durableQueue(t, db, Options{Workers: 1, MaxAttempts: 2, BaseDelay: time.Millisecond, Client: client})
	stageDurable(t, db, q, Event{Type: "host", ID: 1, Action: ActionCreated})
	awaitDurable(t, func() bool { return emptyOutbox(db) })
	if redirects.Load() != 0 || attempts.Load() != 2 {
		t.Fatalf("redirect/retry policy violated: redirected=%d attempts=%d", redirects.Load(), attempts.Load())
	}
	if client.Timeout != 0 || client.CheckRedirect != nil {
		t.Fatal("mutated caller's HTTP client")
	}
}
