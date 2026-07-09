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
	Workers     int           // concurrent delivery workers (default 4)
	QueueSize   int           // buffered event/job channel size (default 256)
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
// after MaxAttempts the job is dropped and logged. In-flight jobs are lost if
// the process exits.
type Queue struct {
	repo   Lister
	client *http.Client
	log    *log.Logger
	max    int
	base   time.Duration
	events chan Event
	jobs   chan *deliveryJob
	wg     sync.WaitGroup
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
	client := opts.Client
	if client == nil {
		client = &http.Client{
			Timeout: opts.Timeout,
			// Refuse to follow redirects: an endpoint answering 3xx would
			// otherwise turn the signed POST into a bodyless GET that "succeeds".
			// Returning the 3xx response makes delivery fail honestly and retry.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}
	q := &Queue{
		repo: repo, client: client, log: logger,
		max: opts.MaxAttempts, base: opts.BaseDelay,
		events: make(chan Event, opts.QueueSize),
		jobs:   make(chan *deliveryJob, opts.QueueSize),
	}
	go q.expander()
	for i := 0; i < opts.Workers; i++ {
		go q.deliveryWorker()
	}
	return q
}

// Dispatch enqueues events without blocking. A full event queue drops the event
// (logged) — never blocks the caller's request or transaction.
func (q *Queue) Dispatch(events ...Event) {
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
// scheduled retries) have terminated. Intended for tests and graceful shutdown.
func (q *Queue) Wait() { q.wg.Wait() }

// expander turns each event into per-endpoint delivery jobs. It runs off the
// caller so the DB read and signing never block Dispatch.
func (q *Queue) expander() {
	for e := range q.events {
		q.expand(e)
		q.wg.Done() // the event's own slot; each job adds its own slot in expand
	}
}

func (q *Queue) expand(e Event) {
	endpoints, err := q.repo.ListEnabled()
	if err != nil {
		q.log.Printf("webhook: list endpoints: %v", err)
		return
	}
	for _, ep := range endpoints {
		if !ep.Matches(e.Type, e.Action) {
			continue
		}
		deliveryID, err := newDeliveryID()
		if err != nil {
			q.log.Printf("webhook: %v", err)
			continue
		}
		body, sig, err := buildBody(e, deliveryID, ep.Secret)
		if err != nil {
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
	select {
	case q.jobs <- j:
	default:
		q.wg.Done()
		q.log.Printf("webhook: job queue full, dropping delivery to %s (%s)", j.url, j.logRef)
	}
}

// deliveryWorker makes one HTTP attempt per job. On failure with attempts
// remaining it schedules a re-enqueue after a backoff delay via a timer, which
// holds the job's wg slot across the delay without occupying a worker.
func (q *Queue) deliveryWorker() {
	for j := range q.jobs {
		err := q.post(j.url, j.body, j.sig, j.deliveryID)
		if err == nil {
			q.wg.Done()
			continue
		}
		q.log.Printf("webhook: deliver to %s attempt %d/%d: %v", j.url, j.attempt, q.max, err)
		if j.attempt >= q.max {
			q.log.Printf("webhook: giving up on %s after %d attempts (%s)", j.url, q.max, j.logRef)
			q.wg.Done()
			continue
		}
		delay := q.base * time.Duration(int64(1)<<(j.attempt-1))
		j.attempt++
		jb := j // bind for the timer closure (safe on any Go version)
		time.AfterFunc(delay, func() { q.enqueue(jb) })
	}
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
