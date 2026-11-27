package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestWithTxContextCancellationRollsBackCompletedWrites(t *testing.T) {
	for _, callbackFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "nil callback error", true: "callback error"}[callbackFails], func(t *testing.T) {
			db := newTestDB(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("callback failed")
			err := WithTxContext(ctx, db, func(tx *sql.Tx) error {
				if _, err := NewHostRepo(db).WithTx(tx).Create(domain.Host{Name: "must roll back", Type: "vm"}); err != nil {
					return err
				}
				cancel()
				if callbackFails {
					return sentinel
				}
				return nil
			})
			if !errors.Is(err, context.Canceled) || (callbackFails && !errors.Is(err, sentinel)) {
				t.Fatalf("cancellation/callback identity lost: %v", err)
			}
			if rows, err := NewHostRepo(db).List(); err != nil || len(rows) != 0 {
				t.Fatalf("canceled transaction committed: %+v %v", rows, err)
			}
		})
	}
}

func TestContextPortabilityRejectsCanceledRequests(t *testing.T) {
	db := newTestDB(t)
	hosts := NewHostRepo(db)
	if _, err := hosts.Create(domain.Host{Name: "keep", Type: "vm"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ImportContext(ctx, db, Snapshot{Version: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled import: %v", err)
	}
	if snapshot, err := ExportContext(ctx, db); !errors.Is(err, context.Canceled) || snapshot.Version != 0 || len(snapshot.Hosts) != 0 {
		t.Fatalf("canceled export returned a snapshot: %+v %v", snapshot, err)
	}
	if rows, err := hosts.List(); err != nil || len(rows) != 1 || rows[0].Name != "keep" {
		t.Fatalf("canceled import changed inventory: %+v %v", rows, err)
	}
	called := false
	if err := WithTxContext(ctx, db, func(*sql.Tx) error { called = true; return nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled context entered transaction: called=%v error=%v", called, err)
	}
}
