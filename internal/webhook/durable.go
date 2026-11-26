package webhook

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Dealisto/almanaut/internal/redact"
	"github.com/Dealisto/almanaut/internal/store"
)

const (
	DefaultOutboxCapacity = 10000
	MaxOutboxBytes        = 64 << 20
	durablePollInterval   = 250 * time.Millisecond
)

// DurableQueue delivers committed outbox rows with at-least-once semantics.
// Stage must run in the entity's write transaction; Dispatch only wakes workers.
// A receiver must deduplicate by X-Almanaut-Delivery: a process can exit after
// the receiver accepts a request but before its completion is persisted.
type DurableQueue struct {
	db       *sql.DB
	repo     *store.WebhookOutboxRepo
	client   *http.Client
	log      *log.Logger
	capacity int
	max      int
	base     time.Duration
	lease    time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	closed   atomic.Bool
	wake     chan struct{}
	done     chan struct{}
	workers  sync.WaitGroup
}

// NewDurableQueue resumes pending deliveries immediately, including expired
// leases from a crashed process. QueueSize bounds persisted deliveries (default
// 10,000), in addition to the 64 MiB total request-content ceiling. Options otherwise
// have the same defaults as NewQueue. No process-local queue can drop events.
func NewDurableQueue(db *sql.DB, opts Options) *DurableQueue {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = DefaultOutboxCapacity
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}
	if opts.BaseDelay <= 0 {
		opts.BaseDelay = 2 * time.Second
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	client := &http.Client{Timeout: opts.Timeout}
	if opts.Client != nil {
		copy := *opts.Client
		client = &copy
		if client.Timeout <= 0 {
			client.Timeout = opts.Timeout
		}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &DurableQueue{
		db: db, repo: store.NewWebhookOutboxRepo(db), client: client, log: logger,
		capacity: opts.QueueSize, max: opts.MaxAttempts, base: opts.BaseDelay,
		lease: client.Timeout + 30*time.Second, ctx: ctx, cancel: cancel,
		wake: make(chan struct{}, opts.Workers), done: make(chan struct{}),
	}
	for range opts.Workers {
		q.workers.Go(q.worker)
	}
	go func() {
		q.workers.Wait()
		q.client.CloseIdleConnections()
		close(q.done)
	}()
	return q
}

func (q *DurableQueue) Stage(tx *sql.Tx, events ...Event) error {
	if tx == nil {
		return errors.New("webhook staging requires an inventory transaction")
	}
	if q.closed.Load() {
		return errors.New("webhook dispatcher is shutting down")
	}
	endpoints, err := store.NewWebhookRepo(q.db).WithTx(tx).ListEnabled()
	if err != nil {
		return err
	}
	repo := q.repo.WithTx(tx)
	for _, event := range events {
		for _, endpoint := range endpoints {
			if !endpoint.Matches(event.Type, event.Action) {
				continue
			}
			id, err := newDeliveryID()
			if err != nil {
				return err
			}
			body, signature, err := buildBody(event, id, endpoint.Secret)
			if err != nil {
				return err
			}
			if err := repo.Enqueue(store.WebhookDelivery{ID: id, WebhookID: endpoint.ID, URL: endpoint.URL, Body: body, Signature: signature}, time.Now(), q.capacity, MaxOutboxBytes); err != nil {
				return err
			}
		}
	}
	return nil
}

func (q *DurableQueue) Dispatch(...Event) {
	for range cap(q.wake) {
		select {
		case q.wake <- struct{}{}:
		default:
			return
		}
	}
}

// Shutdown cancels HTTP work and leaves unfinished deliveries for the next
// enabled process. Delayed retries never extend shutdown. A cancellation while
// persisting cleanup is harmless: its durable lease will expire after a crash.
func (q *DurableQueue) Shutdown(ctx context.Context) error {
	q.closed.Store(true)
	q.cancel()
	select {
	case <-q.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *DurableQueue) worker() {
	ticker := time.NewTicker(durablePollInterval)
	defer ticker.Stop()
	for q.ctx.Err() == nil {
		token, err := newDeliveryID()
		if err == nil {
			var delivery store.WebhookDelivery
			delivery, err = q.repo.Claim(time.Now(), q.lease, token)
			if err == nil {
				q.deliver(delivery)
				continue
			}
		}
		if !errors.Is(err, store.ErrNotFound) && q.ctx.Err() == nil {
			q.log.Printf("webhook: claim delivery: %v", redact.Error(err))
		}
		select {
		case <-q.ctx.Done():
			return
		case <-q.wake:
		case <-ticker.C:
		}
	}
}

func (q *DurableQueue) deliver(d store.WebhookDelivery) {
	var err error
	if q.ctx.Err() != nil {
		err = q.repo.Retry(d, time.Now(), true)
	} else if d.Attempts > q.max {
		q.log.Printf("webhook: giving up delivery %s: recorded attempt %d exceeds retry budget %d", d.ID, d.Attempts, q.max)
		err = q.repo.Complete(d)
	} else {
		active, checkErr := q.repo.Active(d)
		if checkErr != nil {
			err = checkErr // keep the lease for automatic recovery
		} else if active {
			err = q.attempt(d)
		}
	}
	if err != nil {
		q.log.Printf("webhook: persist delivery %s: %v", d.ID, redact.Error(err))
	}
}

func (q *DurableQueue) attempt(d store.WebhookDelivery) error {
	err := func() (err error) {
		defer func() {
			if recover() != nil {
				// An injected transport may panic with credentials in its value.
				err = errors.New("webhook transport panicked")
			}
		}()
		return postDelivery(q.ctx, q.client, d.URL, d.Body, d.Signature, d.ID)
	}()
	if err == nil {
		return q.repo.Complete(d)
	}
	if q.ctx.Err() != nil {
		return q.repo.Retry(d, time.Now(), true)
	}
	q.log.Printf("webhook: deliver to %s attempt %d/%d: %v", redact.URL(d.URL), d.Attempts, q.max, err)
	if d.Attempts >= q.max {
		q.log.Printf("webhook: giving up delivery %s after %d attempts", d.ID, q.max)
		return q.repo.Complete(d)
	}
	if err := q.repo.Retry(d, time.Now().Add(retryDelay(q.base, d.Attempts)), false); err != nil {
		return fmt.Errorf("schedule retry: %w", err)
	}
	return nil
}
