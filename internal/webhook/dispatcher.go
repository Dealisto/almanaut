package webhook

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/redact"
)

// Dispatcher receives committed entity-change events for outbound delivery.
type Dispatcher interface {
	Dispatch(events ...Event)
}

// Noop discards events; injected when webhooks are disabled so call sites can
// dispatch unconditionally.
type Noop struct{}

// Dispatch does nothing.
func (Noop) Dispatch(...Event) {}

// Lister supplies the currently-enabled endpoints. *store.WebhookRepo satisfies it.
type Lister interface {
	ListEnabled() ([]domain.Webhook, error)
}

// Options configures Queue or DurableQueue. Zero fields fall back to defaults.
type Options struct {
	Workers     int           // concurrent delivery workers (default 4)
	QueueSize   int           // retained-delivery capacity (default 256 in Queue, 10,000 in DurableQueue)
	MaxAttempts int           // delivery attempts before giving up (default 5)
	BaseDelay   time.Duration // first retry backoff, doubled each attempt (default 2s)
	Timeout     time.Duration // per-request HTTP timeout (default 10s)
	Client      *http.Client  // overrides the default client (tests inject one)
	Logger      *log.Logger   // defaults to log.Default()
}

// Queue is an in-memory, non-blocking Dispatcher. Dispatch enqueues events; an
// expander turns each into per-endpoint delivery jobs, and a worker pool makes
// one HTTP attempt per job. A failed job is re-enqueued after a backoff delay
// via a timer that never occupies a worker, so a slow/broken receiver cannot
// block delivery to healthy endpoints or to other events. Retries are bounded;
// after MaxAttempts the job is dropped and logged. QueueSize + Workers bounds
// all retained deliveries, including delayed retries. In-flight jobs are lost
// if the process exits. Call Shutdown to drain and stop the queue.
type Queue struct {
	repo   Lister
	client *http.Client
	log    *log.Logger
	max    int
	base   time.Duration
	events chan Event
	jobs   chan *deliveryJob
	wg     sync.WaitGroup
	slots  chan struct{} // all queued, active and delayed deliveries

	ctx      context.Context
	cancel   context.CancelFunc
	workers  sync.WaitGroup
	done     chan struct{}
	mu       sync.Mutex // serializes admission with shutdown
	closed   bool
	stopOnce sync.Once
	retryMu  sync.Mutex
	retries  map[*time.Timer]struct{}
}

type deliveryJob struct {
	url        string
	body       []byte
	sig        string
	deliveryID string
	attempt    int
	logRef     string // e.g. "created host/42", for log lines
}

// NewQueue builds a Queue and starts its expander and worker goroutines.
func NewQueue(repo Lister, opts Options) *Queue {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = 256
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
	// Keep injected transports while applying the same delivery policy. A
	// redirect can otherwise turn the signed POST into a bodyless GET.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{
		repo: repo, client: client, log: logger,
		max: opts.MaxAttempts, base: opts.BaseDelay,
		events: make(chan Event, opts.QueueSize),
		jobs:   make(chan *deliveryJob, opts.QueueSize),
		slots:  make(chan struct{}, opts.QueueSize+opts.Workers),
		ctx:    ctx, cancel: cancel, done: make(chan struct{}),
		retries: make(map[*time.Timer]struct{}),
	}
	q.workers.Go(q.expander)
	for i := 0; i < opts.Workers; i++ {
		q.workers.Go(q.deliveryWorker)
	}
	return q
}

// Dispatch enqueues events without blocking. A full event queue drops the event
// (logged) — never blocks the caller's request or transaction.
func (q *Queue) Dispatch(events ...Event) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	for _, e := range events {
		q.wg.Add(1)
		select {
		case q.events <- e:
		default:
			q.wg.Done()
			q.log.Printf("webhook: event queue full, dropping %s %s/%d", e.Action, e.Type, e.ID)
		}
	}
}

// Wait blocks until every enqueued event and all of its delivery jobs (including
// scheduled retries) have terminated. Callers must stop dispatching before
// waiting. Use Shutdown to also stop admission and release worker goroutines.
func (q *Queue) Wait() { q.wg.Wait() }

// Shutdown stops accepting events and drains accepted work until ctx ends.
// At the deadline it cancels HTTP requests and pending retries. It is safe to
// call again to wait for cleanup; workers and timers stop once accepted work
// has terminated. Dispatch after shutdown is ignored.
func (q *Queue) Shutdown(ctx context.Context) error {
	q.stopOnce.Do(func() {
		q.mu.Lock()
		q.closed = true
		q.mu.Unlock()
		go func() {
			q.wg.Wait()
			q.cancel()
			close(q.events)
			close(q.jobs)
			q.workers.Wait()
			q.client.CloseIdleConnections()
			close(q.done)
		}()
	})
	select {
	case <-q.done:
		return nil
	case <-ctx.Done():
		q.cancel()
		q.retryMu.Lock()
		for timer := range q.retries {
			if timer.Stop() {
				delete(q.retries, timer)
				q.finishJob()
			}
		}
		q.retryMu.Unlock()
		return ctx.Err()
	}
}

// expander turns each event into per-endpoint delivery jobs. It runs off the
// caller so the DB read and signing never block Dispatch.
func (q *Queue) expander() {
	for e := range q.events {
		if q.ctx.Err() == nil {
			q.safely("expand "+e.Type, func() { q.expand(e) })
		}
		q.wg.Done() // the event's own slot; each job adds its own slot in expand
	}
}

// safely runs fn, logging a panic instead of letting it kill the process: the
// queue's goroutines are not under the HTTP server's recover middleware, and a
// dead worker would also strand Wait.
func (q *Queue) safely(what string, fn func()) {
	defer func() {
		if p := recover(); p != nil {
			q.log.Printf("webhook: %s panicked: %v\n%s", what, p, debug.Stack())
		}
	}()
	fn()
}

func (q *Queue) expand(e Event) {
	endpoints, err := q.repo.ListEnabled()
	if err != nil {
		q.log.Printf("webhook: list endpoints: %v", err)
		return
	}
	for _, ep := range endpoints {
		if q.ctx.Err() != nil {
			return
		}
		if !ep.Matches(e.Type, e.Action) {
			continue
		}
		select {
		case q.slots <- struct{}{}:
		default:
			q.log.Printf("webhook: delivery capacity full, dropping %s %s/%d", e.Action, e.Type, e.ID)
			continue
		}
		deliveryID, err := newDeliveryID()
		if err != nil {
			<-q.slots
			q.log.Printf("webhook: %v", err)
			continue
		}
		body, sig, err := buildBody(e, deliveryID, ep.Secret)
		if err != nil {
			<-q.slots
			q.log.Printf("webhook: %v", err)
			continue
		}
		q.wg.Add(1) // job slot, added before the event's wg.Done in expander
		q.enqueue(&deliveryJob{
			url: ep.URL, body: body, sig: sig, deliveryID: deliveryID,
			attempt: 1, logRef: fmt.Sprintf("%s %s/%d", e.Action, e.Type, e.ID),
		})
	}
}

// enqueue pushes a job without blocking; a full job queue drops it (logged) and
// releases its wg slot.
func (q *Queue) enqueue(j *deliveryJob) {
	if q.ctx.Err() != nil {
		q.finishJob()
		return
	}
	select {
	case q.jobs <- j:
	default:
		q.finishJob()
		q.log.Printf("webhook: job queue full, dropping delivery to %s (%s)", redact.URL(j.url), j.logRef)
	}
}

func (q *Queue) finishJob() {
	<-q.slots
	q.wg.Done()
}

func (q *Queue) retry(j *deliveryJob, delay time.Duration) {
	q.retryMu.Lock()
	defer q.retryMu.Unlock()
	if q.ctx.Err() != nil {
		q.finishJob()
		return
	}
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		q.retryMu.Lock()
		delete(q.retries, timer)
		q.retryMu.Unlock()
		q.enqueue(j)
	})
	q.retries[timer] = struct{}{}
}

// deliveryWorker makes one HTTP attempt per job. On failure with attempts
// remaining it schedules a re-enqueue after a backoff delay via a timer, which
// holds the job's wg slot across the delay without occupying a worker.
func (q *Queue) deliveryWorker() {
	for j := range q.jobs {
		if q.ctx.Err() != nil {
			q.finishJob()
			continue
		}
		err := fmt.Errorf("delivery panicked")
		q.safely("delivery", func() { err = q.post(j.url, j.body, j.sig, j.deliveryID) })
		if err == nil {
			q.finishJob()
			continue
		}
		q.log.Printf("webhook: deliver to %s attempt %d/%d: %v", redact.URL(j.url), j.attempt, q.max, err)
		if j.attempt >= q.max {
			q.log.Printf("webhook: giving up on %s after %d attempts (%s)", redact.URL(j.url), q.max, j.logRef)
			q.finishJob()
			continue
		}
		delay := retryDelay(q.base, j.attempt)
		j.attempt++
		q.retry(j, delay)
	}
}

// Saturate instead of overflowing into a negative delay and a tight retry loop.
func retryDelay(base time.Duration, attempt int) time.Duration {
	const maxDelay = time.Duration(1<<63 - 1)
	for i := 1; i < attempt; i++ {
		if base > maxDelay/2 {
			return maxDelay
		}
		base *= 2
	}
	return base
}

func (q *Queue) post(url string, body []byte, sig, deliveryID string) error {
	return postDelivery(q.ctx, q.client, url, body, sig, deliveryID)
}

func postDelivery(ctx context.Context, client *http.Client, url string, body []byte, sig, deliveryID string) error {
	// Errors carry the endpoint URL, which often embeds a token: redact them,
	// since the caller logs every failed attempt.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return redact.ErrorWithURL(err, url, sig)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Almanaut-Signature", sig)
	req.Header.Set("X-Almanaut-Delivery", deliveryID)
	resp, err := client.Do(req)
	if err != nil {
		return redact.ErrorWithURL(err, url, sig)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// Multi fans events out to several dispatchers (e.g. the delivery Queue and
// the Uptime Kuma sync trigger). Each dispatcher must itself be non-blocking.
type Multi []Dispatcher

// Dispatch forwards the events to every dispatcher in order.
func (m Multi) Dispatch(events ...Event) {
	for _, d := range m {
		d.Dispatch(events...)
	}
}

// Stage persists deliveries before the inventory transaction commits. Legacy
// dispatchers have no durable state and are still notified after commit.
func Stage(tx *sql.Tx, dispatcher Dispatcher, events ...Event) error {
	if staged, ok := dispatcher.(interface {
		Stage(*sql.Tx, ...Event) error
	}); ok {
		return staged.Stage(tx, events...)
	}
	return nil
}

func (m Multi) Stage(tx *sql.Tx, events ...Event) error {
	for _, dispatcher := range m {
		if err := Stage(tx, dispatcher, events...); err != nil {
			return err
		}
	}
	return nil
}
