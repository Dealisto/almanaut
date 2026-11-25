// Package liveness runs lightweight TCP reachability checks against hosts and
// services and records the result, notifying on each up/down transition.
package liveness

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/notify"
)

// HostSource / ServiceSource / StateStore are the repo subsets the checker needs.
type HostSource interface {
	List() ([]domain.Host, error)
}
type ServiceSource interface {
	List() ([]domain.Service, error)
}
type StateStore interface {
	RecordCheck(entityType string, entityID int64, address string, s domain.LivenessStatus) (*domain.LivenessStatus, bool, error)
}

// NotificationQueue is implemented by the persistent state repository. Lightweight
// test adapters may omit it and retain immediate transition delivery.
type NotificationQueue interface {
	PendingNotifications(runID string, limit int) ([]domain.LivenessNotification, error)
	NotificationCurrent(id int64) (bool, error)
	MarkNotificationAttempt(id int64, runID string) (bool, error)
	DeleteNotification(id int64) error
}

// DialFunc probes one address; nil error means reachable. Overridable in tests.
type DialFunc func(ctx context.Context, addr string) error

// maxConcurrentChecks bounds how many dials one pass runs at once. Dialing one
// target at a time let a handful of unreachable addresses (each waiting out
// the full timeout) use up the whole pass, so the targets after them were
// never checked again and their status froze.
const maxConcurrentChecks = 16

// Checker performs one pass over every monitored host and service per Run.
type Checker struct {
	hosts    HostSource
	services ServiceSource
	state    StateStore
	sender   notify.Sender
	dial     DialFunc
	timeout  time.Duration
	log      *slog.Logger
	now      func() time.Time
	workers  int           // concurrent dials per pass
	runGate  chan struct{} // context-aware admission for concurrent Run callers
	nextType string        // first target not started during the previous pass
	nextID   int64
}

// New builds a Checker. dial may be nil to use the default TCP dialer; log may
// be nil (defaults to slog.Default); now may be nil (defaults to time.Now).
func New(hosts HostSource, services ServiceSource, state StateStore, sender notify.Sender,
	dial DialFunc, timeout time.Duration, log *slog.Logger, now func() time.Time) *Checker {
	if dial == nil {
		dial = tcpDial
	}
	if log == nil {
		log = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	return &Checker{hosts: hosts, services: services, state: state, sender: sender, dial: dial, timeout: timeout, log: log, now: now, workers: maxConcurrentChecks, runGate: make(chan struct{}, 1)}
}

func tcpDial(ctx context.Context, addr string) error {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// target is one monitored address.
type target struct {
	entityType string
	id         int64
	label      string
	addr       string
}

// Run probes every monitored host/service once, up to c.workers at a time. A
// single entity's error is logged and does not abort the pass. Storage and
// notification errors are returned together after all checks have finished.
func (c *Checker) Run(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c.runGate <- struct{}{}:
	}
	defer func() { <-c.runGate }()
	if err := ctx.Err(); err != nil {
		return err
	}
	hosts, err := c.hosts.List()
	if err != nil {
		return fmt.Errorf("list hosts: %w", err)
	}
	services, err := c.services.List()
	if err != nil {
		return fmt.Errorf("list services: %w", err)
	}
	var targets []target
	for _, h := range hosts {
		if h.CheckAddress != "" {
			targets = append(targets, target{"host", h.ID, h.Name, h.CheckAddress})
		}
	}
	for _, s := range services {
		if s.CheckAddress != "" {
			targets = append(targets, target{"service", s.ID, s.Name, s.CheckAddress})
		}
	}
	// Rotate by stable identity rather than name so a pass deadline cannot
	// leave the same hosts or all services unchecked on every run.
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].entityType != targets[j].entityType {
			return targets[i].entityType < targets[j].entityType
		}
		return targets[i].id < targets[j].id
	})
	start := sort.Search(len(targets), func(i int) bool {
		return targets[i].entityType > c.nextType ||
			(targets[i].entityType == c.nextType && targets[i].id >= c.nextID)
	})
	if start == len(targets) {
		start = 0
	}

	// Delivery and observation progress independently: a slow notification
	// endpoint must not consume the whole pass before any TCP checks start.
	queue, durable := c.state.(NotificationQueue)
	runID := rand.Text()
	budget := 100
	deliveryDone := make(chan error, 1)
	if durable {
		go func() { deliveryDone <- c.safeDrain(ctx, queue, runID, &budget) }()
	}
	sem := make(chan struct{}, max(c.workers, 1))
	checkErrors := make([]error, len(targets))
	var wg sync.WaitGroup
dispatch:
	for i := range targets {
		t := targets[(start+i)%len(targets)]
		c.nextType, c.nextID = t.entityType, t.id
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			break dispatch // the pass is over: start no further dials
		case sem <- struct{}{}:
		}
		if ctx.Err() != nil {
			<-sem
			break
		}
		next := targets[(start+i+1)%len(targets)]
		c.nextType, c.nextID = next.entityType, next.id
		wg.Go(func() {
			defer func() { <-sem }()
			checkErrors[i] = c.safeCheck(ctx, t)
		})
	}
	wg.Wait()
	var deliveryErr error
	if durable {
		deliveryErr = <-deliveryDone
		// The initial drain may have finished before these checks enqueued new
		// transitions. Joining it also synchronizes access to the shared budget.
		deliveryErr = errors.Join(deliveryErr, c.safeDrain(ctx, queue, runID, &budget))
	}
	return errors.Join(append(checkErrors, deliveryErr, ctx.Err())...)
}

// safeCheck runs check for t and turns a panic into a logged error. The job
// runner recovers panics in Run itself, but not in goroutines Run starts; one
// bad target must not take the whole process down.
func (c *Checker) safeCheck(ctx context.Context, t target) (err error) {
	defer func() {
		if p := recover(); p != nil {
			c.log.Error("liveness: check panicked", "type", t.entityType, "id", t.id, "panic", p, "stack", string(debug.Stack()))
			err = fmt.Errorf("check %s %d panicked: %v", t.entityType, t.id, p)
		}
	}()
	return c.check(ctx, t.entityType, t.id, t.label, t.addr)
}

func (c *Checker) check(ctx context.Context, entityType string, id int64, label, addr string) error {
	if ctx.Err() != nil {
		return nil
	}
	startedAt := c.now()
	dialCtx, cancel := context.WithTimeout(ctx, c.timeout)
	probeErr := c.dial(dialCtx, addr)
	cancel()
	if ctx.Err() != nil {
		return nil // parent context ended mid-dial; don't record or notify a spurious result
	}

	status := domain.LivenessUp
	lastErr := ""
	if probeErr != nil {
		status = domain.LivenessDown
		lastErr = probeErr.Error()
	}

	prior, accepted, err := c.state.RecordCheck(entityType, id, addr, domain.LivenessStatus{
		Status: status, CheckedAt: startedAt, LastError: lastErr,
	})
	if err != nil {
		c.log.Error("liveness: record check", "type", entityType, "id", id, "err", err)
		return fmt.Errorf("record %s %d check: %w", entityType, id, err)
	}

	// Notify only on a real transition (a prior observation existed and differs).
	if _, durable := c.state.(NotificationQueue); !durable && accepted && prior != nil && prior.Status != status {
		if err := c.notify(ctx, entityType, label, addr, status, lastErr); err != nil {
			return fmt.Errorf("notify %s %d transition: %w", entityType, id, err)
		}
	}
	return nil
}

func (c *Checker) notify(ctx context.Context, entityType, label, addr, status, lastErr string, observedAt ...string) error {
	var n notify.Notification
	if status == domain.LivenessDown {
		n = notify.Notification{
			Title: fmt.Sprintf("%s %q is DOWN", entityType, label),
			Body:  fmt.Sprintf("%s is unreachable at %s: %s", label, addr, lastErr),
			Tags:  "warning",
		}
	} else {
		n = notify.Notification{
			Title: fmt.Sprintf("%s %q recovered", entityType, label),
			Body:  fmt.Sprintf("%s is reachable again at %s", label, addr),
		}
	}
	if len(observedAt) > 0 {
		n.Body += " (observed " + observedAt[0] + ")"
	}
	if err := c.sender.Send(ctx, n); err != nil {
		c.log.Error("liveness: send notification", "label", label, "err", err)
		return err
	}
	return nil
}

// drain preserves FIFO order per entity while allowing other entities to proceed
// after one delivery fails. Persisted attempt metadata makes retries fair across
// passes and prevents attempting a failed event twice in the same pass.
func (c *Checker) drain(ctx context.Context, queue NotificationQueue, runID string, budget *int) error {
	var result error
	for *budget > 0 {
		if err := ctx.Err(); err != nil {
			return errors.Join(result, err)
		}
		pending, err := queue.PendingNotifications(runID, *budget)
		if err != nil {
			return errors.Join(result, err)
		}
		if len(pending) == 0 {
			return result
		}
		for _, n := range pending {
			if err := ctx.Err(); err != nil {
				return errors.Join(result, err)
			}
			*budget--
			current, err := queue.NotificationCurrent(n.ID)
			if err != nil {
				result = errors.Join(result, err)
				continue
			}
			if current {
				marked, err := queue.MarkNotificationAttempt(n.ID, runID)
				if err != nil {
					result = errors.Join(result, err)
					continue
				}
				if !marked {
					continue
				}
				if err := c.notify(ctx, n.EntityType, n.Label, n.Address, n.Status, n.LastError, n.ObservedAt); err != nil {
					result = errors.Join(result, fmt.Errorf("deliver liveness notification %d: %w", n.ID, err))
					continue
				}
			}
			if err := queue.DeleteNotification(n.ID); err != nil {
				result = errors.Join(result, fmt.Errorf("acknowledge liveness notification %d: %w", n.ID, err))
			}
		}
	}
	return result
}

// Delivery now runs alongside probe workers, so recover its panics locally.
func (c *Checker) safeDrain(ctx context.Context, queue NotificationQueue, runID string, budget *int) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("liveness delivery panicked: %v", recovered)
			c.log.Error("liveness delivery panic", "panic", recovered, "stack", string(debug.Stack()))
		}
	}()
	return c.drain(ctx, queue, runID, budget)
}
