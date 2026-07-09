package kuma

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/webhook"
)

// passTimeout bounds one reconcile pass so a hung Kuma cannot wedge the loop.
const passTimeout = 30 * time.Second

// ServiceLister is the slice of store.ServiceRepo the syncer needs.
type ServiceLister interface {
	List() ([]domain.Service, error)
}

// mappingStore is the slice of *store.KumaRepo the syncer needs.
type mappingStore interface {
	All() (map[int64]int64, error)
	Put(serviceID, monitorID int64) error
	Delete(serviceID int64) error
}

// Summary reports what one reconcile pass did.
type Summary struct {
	Created, Updated, Deleted, Skipped int
	Err                                error
}

// LastSync is the outcome of the most recent pass, shown on the admin page.
type LastSync struct {
	Ran     bool
	Time    time.Time
	Summary Summary
}

// Syncer runs idempotent reconcile passes: almanaut services in, Kuma monitor
// CRUD out, with the kuma_monitors table recording what almanaut manages.
type Syncer struct {
	client   *Client
	services ServiceLister
	mapping  mappingStore
	log      *log.Logger

	trigger chan struct{} // size 1: pending-work flag, coalesces bursts

	mu   sync.Mutex
	last LastSync
}

// NewSyncer wires a syncer; Start must be called (in a goroutine) to serve
// triggers. logger nil defaults to log.Default().
func NewSyncer(client *Client, services ServiceLister, mapping mappingStore, logger *log.Logger) *Syncer {
	if logger == nil {
		logger = log.Default()
	}
	return &Syncer{
		client: client, services: services, mapping: mapping, log: logger,
		trigger: make(chan struct{}, 1),
	}
}

// TriggerSync requests a reconcile pass without blocking. A trigger arriving
// while a pass runs is buffered (one slot) so exactly one follow-up pass
// happens; further triggers coalesce into it.
func (y *Syncer) TriggerSync() {
	select {
	case y.trigger <- struct{}{}:
	default:
	}
}

// Dispatch implements webhook.Dispatcher: any committed service change
// requests a sync. Other entity types are ignored.
func (y *Syncer) Dispatch(events ...webhook.Event) {
	for _, e := range events {
		if e.Type == "service" {
			y.TriggerSync()
			return
		}
	}
}

// LastSync returns the outcome of the most recent pass.
func (y *Syncer) LastSync() LastSync {
	y.mu.Lock()
	defer y.mu.Unlock()
	return y.last
}

// Start runs one initial pass, then a pass per trigger, until ctx is
// cancelled. Call it in a goroutine; it blocks.
func (y *Syncer) Start(ctx context.Context) {
	y.pass(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-y.trigger:
			y.pass(ctx)
		}
	}
}

func (y *Syncer) pass(ctx context.Context) {
	runCtx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()
	sum := y.Reconcile(runCtx)
	if sum.Err != nil {
		y.log.Printf("kuma: sync: %v", sum.Err)
	} else {
		y.log.Printf("kuma: sync ok (created %d, updated %d, deleted %d, skipped %d)",
			sum.Created, sum.Updated, sum.Deleted, sum.Skipped)
	}
}

// Reconcile runs one synchronous pass and records it as the last sync. A
// mapping row only changes after the corresponding Kuma operation succeeded,
// so a mid-pass failure leaves a state the next pass repairs.
func (y *Syncer) Reconcile(ctx context.Context) Summary {
	sum := y.reconcile(ctx)
	y.mu.Lock()
	y.last = LastSync{Ran: true, Time: time.Now(), Summary: sum}
	y.mu.Unlock()
	return sum
}

func (y *Syncer) reconcile(ctx context.Context) Summary {
	var sum Summary
	services, err := y.services.List()
	if err != nil {
		sum.Err = err
		return sum
	}
	mapping, err := y.mapping.All()
	if err != nil {
		sum.Err = err
		return sum
	}

	session, err := y.client.Connect(ctx)
	if err != nil {
		sum.Err = err
		return sum
	}
	defer session.Close()

	actions, skipped := plan(services, mapping, session.Monitors())
	sum.Skipped = skipped
	for _, a := range actions {
		if err := ctx.Err(); err != nil {
			sum.Err = err
			return sum
		}
		switch a.kind {
		case actCreate:
			id, err := session.Add(ctx, a.monitor)
			if err != nil {
				sum.Err = err
				return sum
			}
			if err := y.mapping.Put(a.serviceID, id); err != nil {
				// The monitor exists in Kuma but almanaut failed to record it. Delete
				// it again so a re-run cannot create a duplicate; if the cleanup also
				// fails, log the orphan loudly for manual removal.
				if derr := session.Delete(ctx, id); derr != nil {
					y.log.Printf("kuma: orphaned monitor %d (%q): mapping write failed (%v) and cleanup delete failed (%v)", id, a.monitor.Name, err, derr)
				}
				sum.Err = err
				return sum
			}
			sum.Created++
		case actEdit:
			if err := session.Edit(ctx, a.monitor); err != nil {
				sum.Err = err
				return sum
			}
			sum.Updated++
		case actDelete:
			err := session.Delete(ctx, a.monitor.ID)
			if err == nil {
				err = y.mapping.Delete(a.serviceID)
			}
			if err != nil {
				sum.Err = err
				return sum
			}
			sum.Deleted++
		case actForget:
			if err := y.mapping.Delete(a.serviceID); err != nil {
				sum.Err = err
				return sum
			}
		}
	}
	return sum
}
