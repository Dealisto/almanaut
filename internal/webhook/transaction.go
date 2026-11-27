package webhook

import (
	"database/sql"
	"errors"
)

// ForTransaction reuses endpoint configuration and outbox admission accounting
// across successive Stage calls in one inventory transaction. It is intended
// for bulk writers that own all staging in that transaction. Dispatch still
// forwards to the original dispatcher and must happen only after commit.
//
// Use the returned dispatcher on one goroutine, only with tx, and discard it
// when the transaction ends. Do not interleave other outbox/endpoint writes or
// savepoint rollbacks: they would invalidate its cached accounting. Inventory,
// history and indirect mutation writes can proceed normally through this scope.
//
// A single DurableQueue, optionally nested in Multi alongside legacy dispatchers,
// can share accounting safely. Unknown stagers or multiple durable queues retain
// their existing behavior, since they could otherwise invalidate each other's
// budgets. No state is cached on the original dispatcher or across transactions.
func ForTransaction(tx *sql.Tx, dispatcher Dispatcher) Dispatcher {
	var queue *DurableQueue
	var visit func(Dispatcher) bool
	visit = func(d Dispatcher) bool {
		switch d := d.(type) {
		case Multi:
			for _, child := range d {
				if !visit(child) {
					return false
				}
			}
			return true
		case *Multi:
			return d != nil && visit(Multi(*d))
		case *DurableQueue:
			if queue != nil || d == nil {
				return false
			}
			queue = d
			return true
		default:
			_, staged := d.(interface{ Stage(*sql.Tx, ...Event) error })
			return !staged
		}
	}
	if !visit(dispatcher) || queue == nil {
		return dispatcher
	}
	return &transactionDispatcher{
		Dispatcher: dispatcher,
		stage:      durableStage{queue: queue, tx: tx},
	}
}

type transactionDispatcher struct {
	Dispatcher
	stage durableStage
}

func (d *transactionDispatcher) Stage(tx *sql.Tx, events ...Event) error {
	if tx != d.stage.tx {
		return errors.New("webhook staging scope belongs to another transaction")
	}
	return d.stage.stage(events...)
}
