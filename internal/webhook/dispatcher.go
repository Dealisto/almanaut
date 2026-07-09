package webhook

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
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

// Options configures a Queue. Zero fields fall back to defaults.
type Options struct {
	Workers     int           // concurrent event workers (default 4)
	QueueSize   int           // buffered event-channel size (default 256)
	MaxAttempts int           // delivery attempts before giving up (default 5)
	BaseDelay   time.Duration // first retry backoff, doubled each attempt (default 2s)
	Timeout     time.Duration // per-request HTTP timeout (default 10s)
	Client      *http.Client  // overrides the default client (tests inject one)
	Logger      *log.Logger   // defaults to log.Default()
}

// Queue is an in-memory, non-blocking Dispatcher. Dispatch enqueues events onto
// a buffered channel; a worker pool delivers each to every matching enabled
// endpoint, retrying with bounded exponential backoff. In-flight deliveries are
// lost if the process exits.
type Queue struct {
	repo   Lister
	client *http.Client
	log    *log.Logger
	max    int
	base   time.Duration
	ch     chan Event
	wg     sync.WaitGroup
}

// NewQueue builds a Queue and starts its worker goroutines.
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
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: opts.Timeout}
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}
	q := &Queue{
		repo: repo, client: client, log: logger,
		max: opts.MaxAttempts, base: opts.BaseDelay,
		ch: make(chan Event, opts.QueueSize),
	}
	for i := 0; i < opts.Workers; i++ {
		go q.worker()
	}
	return q
}

// Dispatch enqueues events without blocking. If the buffer is full the event is
// dropped and logged (never blocks the caller's request or transaction).
func (q *Queue) Dispatch(events ...Event) {
	for _, e := range events {
		q.wg.Add(1)
		select {
		case q.ch <- e:
		default:
			q.wg.Done()
			q.log.Printf("webhook: queue full, dropping %s %s/%d", e.Action, e.Type, e.ID)
		}
	}
}

// Wait blocks until every enqueued event has been fully processed. Intended for
// tests and graceful shutdown.
func (q *Queue) Wait() { q.wg.Wait() }

func (q *Queue) worker() {
	for e := range q.ch {
		q.process(e)
		q.wg.Done()
	}
}

func (q *Queue) process(e Event) {
	endpoints, err := q.repo.ListEnabled()
	if err != nil {
		q.log.Printf("webhook: list endpoints: %v", err)
		return
	}
	for _, ep := range endpoints {
		if ep.Matches(e.Type, e.Action) {
			q.deliver(ep, e)
		}
	}
}

func (q *Queue) deliver(ep domain.Webhook, e Event) {
	deliveryID, err := newDeliveryID()
	if err != nil {
		q.log.Printf("webhook: %v", err)
		return
	}
	body, sig, err := buildBody(e, deliveryID, ep.Secret)
	if err != nil {
		q.log.Printf("webhook: %v", err)
		return
	}
	for attempt := 1; attempt <= q.max; attempt++ {
		if err := q.post(ep.URL, body, sig, deliveryID); err == nil {
			return
		} else {
			q.log.Printf("webhook: deliver to %s attempt %d/%d: %v", ep.URL, attempt, q.max, err)
		}
		if attempt < q.max {
			time.Sleep(q.base * time.Duration(int64(1)<<(attempt-1)))
		}
	}
	q.log.Printf("webhook: giving up on %s after %d attempts (%s %s/%d)", ep.URL, q.max, e.Action, e.Type, e.ID)
}

func (q *Queue) post(url string, body []byte, sig, deliveryID string) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Almanaut-Signature", sig)
	req.Header.Set("X-Almanaut-Delivery", deliveryID)
	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
