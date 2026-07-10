// Package liveness runs lightweight TCP reachability checks against hosts and
// services and records the result, notifying on each up/down transition.
package liveness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/notify"
	"github.com/Dealisto/almanaut/internal/store"
)

// HostSource / ServiceSource / StateStore are the repo subsets the checker needs.
type HostSource interface {
	List() ([]domain.Host, error)
}
type ServiceSource interface {
	List() ([]domain.Service, error)
}
type StateStore interface {
	Get(entityType string, entityID int64) (domain.LivenessStatus, error)
	Upsert(entityType string, entityID int64, s domain.LivenessStatus) error
}

// DialFunc probes one address; nil error means reachable. Overridable in tests.
type DialFunc func(ctx context.Context, addr string) error

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
	return &Checker{hosts, services, state, sender, dial, timeout, log, now}
}

func tcpDial(ctx context.Context, addr string) error {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// Run probes every monitored host/service once. A single entity's error is
// logged and does not abort the pass. It returns nil unless the context ended.
func (c *Checker) Run(ctx context.Context) error {
	hosts, err := c.hosts.List()
	if err != nil {
		return fmt.Errorf("list hosts: %w", err)
	}
	for _, h := range hosts {
		if h.CheckAddress == "" {
			continue
		}
		c.check(ctx, "host", h.ID, h.Name, h.CheckAddress)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	services, err := c.services.List()
	if err != nil {
		return fmt.Errorf("list services: %w", err)
	}
	for _, s := range services {
		if s.CheckAddress == "" {
			continue
		}
		c.check(ctx, "service", s.ID, s.Name, s.CheckAddress)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

func (c *Checker) check(ctx context.Context, entityType string, id int64, label, addr string) {
	dialCtx, cancel := context.WithTimeout(ctx, c.timeout)
	probeErr := c.dial(dialCtx, addr)
	cancel()

	status := domain.LivenessUp
	lastErr := ""
	if probeErr != nil {
		status = domain.LivenessDown
		lastErr = probeErr.Error()
	}

	prior, err := c.state.Get(entityType, id)
	priorKnown := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		c.log.Error("liveness: read state", "type", entityType, "id", id, "err", err)
		return
	}

	now := c.now()
	changedAt := now
	if priorKnown && prior.Status == status {
		changedAt = prior.ChangedAt // status unchanged: keep original change time
	}
	if err := c.state.Upsert(entityType, id, domain.LivenessStatus{
		Status: status, CheckedAt: now, ChangedAt: changedAt, LastError: lastErr,
	}); err != nil {
		c.log.Error("liveness: upsert state", "type", entityType, "id", id, "err", err)
		return
	}

	// Notify only on a real transition (a prior observation existed and differs).
	if priorKnown && prior.Status != status {
		c.notify(ctx, entityType, label, addr, status, lastErr)
	}
}

func (c *Checker) notify(ctx context.Context, entityType, label, addr, status, lastErr string) {
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
	if err := c.sender.Send(ctx, n); err != nil {
		c.log.Error("liveness: send notification", "label", label, "err", err)
	}
}
