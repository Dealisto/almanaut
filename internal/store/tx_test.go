package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestWithTxCommits(t *testing.T) {
	db := newTestDB(t)
	err := WithTx(db, func(tx *sql.Tx) error {
		_, err := NewServiceRepo(db).WithTx(tx).Create(domain.Service{Name: "jellyfin", Kind: "container"})
		return err
	})
	if err != nil {
		t.Fatalf("WithTx returned %v, want nil", err)
	}
	got, err := NewServiceRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "jellyfin" {
		t.Fatalf("after commit got %+v, want one service jellyfin", got)
	}
}

func TestWithTxRollsBackOnError(t *testing.T) {
	db := newTestDB(t)
	sentinel := errors.New("boom")
	err := WithTx(db, func(tx *sql.Tx) error {
		if _, err := NewServiceRepo(db).WithTx(tx).Create(domain.Service{Name: "jellyfin", Kind: "container"}); err != nil {
			return err
		}
		return sentinel // abort after a successful write
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx returned %v, want sentinel", err)
	}
	got, err := NewServiceRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("after rollback got %+v, want no services", got)
	}
}

func TestWithTxBoundRepoIsolatedUntilCommit(t *testing.T) {
	db := newTestDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := NewServiceRepo(db).WithTx(tx).Create(domain.Service{Name: "sonarr", Kind: "container"}); err != nil {
		t.Fatalf("create in tx: %v", err)
	}
	// A read on the plain DB connection must not see the uncommitted row.
	got, err := NewServiceRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("uncommitted row visible outside tx: %+v", got)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}

// fakeCodedError exposes the same structural Code() int method
// *sqlite.Error does, letting a test simulate any SQLite result code
// (including extended variants) without a real locked database.
type fakeCodedError struct{ code int }

func (e fakeCodedError) Error() string { return "simulated sqlite error" }
func (e fakeCodedError) Code() int     { return e.code }

// TestSqliteBusyOrLockedRecognizesExtendedCodes pins the regression this
// package's flaky agent-report test exposed: a deferred transaction that
// reads before it writes can fail with the extended SQLITE_BUSY_SNAPSHOT
// (517) rather than plain SQLITE_BUSY (5) — see sqliteBusyOrLocked's doc
// comment for how that was confirmed. Every extended variant of BUSY/LOCKED
// must be classified as transient contention, not just the two primary
// codes.
func TestSqliteBusyOrLockedRecognizesExtendedCodes(t *testing.T) {
	const (
		sqliteBusySnapshot      = 517 // SQLITE_BUSY | (2<<8)
		sqliteLockedSharedCache = 262 // SQLITE_LOCKED | (1<<8)
		sqliteBusy              = 5
		sqliteLocked            = 6
		sqliteConstraintUnique  = 2067 // unrelated code: must stay unrecognized
	)
	tests := []struct {
		name string
		code int
		want bool
	}{
		{"plain SQLITE_BUSY", sqliteBusy, true},
		{"plain SQLITE_LOCKED", sqliteLocked, true},
		{"SQLITE_BUSY_SNAPSHOT", sqliteBusySnapshot, true},
		{"SQLITE_LOCKED_SHAREDCACHE", sqliteLockedSharedCache, true},
		{"unrelated SQLITE_CONSTRAINT_UNIQUE", sqliteConstraintUnique, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sqliteBusyOrLocked(fakeCodedError{code: tt.code})
			if got != tt.want {
				t.Fatalf("sqliteBusyOrLocked(code=%d) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestWithTxPanicRollsBack(t *testing.T) {
	db := newTestDB(t)
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		_ = WithTx(db, func(tx *sql.Tx) error {
			if _, err := NewServiceRepo(db).WithTx(tx).Create(domain.Service{Name: "plex", Kind: "container"}); err != nil {
				t.Fatalf("create in tx: %v", err)
			}
			panic("simulated crash")
		})
	}()
	if !panicked {
		t.Fatal("expected the panic to propagate out of WithTx")
	}
	got, err := NewServiceRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("after panic+rollback got %+v, want no services", got)
	}
}

// Two transactions that each read before they write must both succeed. This is
// the regression test for the failure #130 diagnosed: under the deferred
// transactions Open used to produce, both would take a read snapshot, and
// whichever tried to write second lost the lock upgrade outright — instantly,
// with no busy-handler wait, as SQLITE_BUSY (5) or SQLITE_BUSY_SNAPSHOT (517).
// The sleep is what makes that deterministic rather than a race: it guarantees
// both snapshots are taken before either write is attempted, which is exactly
// the interleaving that used to fail.
//
// With _txlock=immediate the second transaction instead blocks at BEGIN until
// the first commits, well inside busy_timeout, and then runs on a fresh
// snapshot. Both writes land.
func TestWithTxReadThenWriteSurvivesContention(t *testing.T) {
	db := newTestDB(t)

	const n = 2
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = WithTx(db, func(tx *sql.Tx) error {
				// Read first: this is what used to poison the transaction.
				if _, err := NewServiceRepo(db).WithTx(tx).List(); err != nil {
					return err
				}
				time.Sleep(50 * time.Millisecond)
				_, err := NewServiceRepo(db).WithTx(tx).Create(
					domain.Service{Name: fmt.Sprintf("svc-%d", i), Kind: "container"})
				return err
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("transaction %d failed: %v (busyOrLocked=%v)", i, err, sqliteBusyOrLocked(err))
		}
	}
	got, err := NewServiceRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != n {
		t.Fatalf("got %d services, want %d — a transaction was lost", len(got), n)
	}
}

// A read-only transaction must not hold the write lock, or every export would
// block every writer for as long as it takes to read all twenty-odd tables.
// This pins the ReadOnly opt-out that Export relies on; without it, the write
// below waits out busy_timeout (5s) and fails.
func TestReadOnlyTxDoesNotBlockWriters(t *testing.T) {
	db := newTestDB(t)

	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()
	if _, err := NewServiceRepo(db).WithTx(tx).List(); err != nil {
		t.Fatalf("read in tx: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := NewServiceRepo(db).Create(domain.Service{Name: "radarr", Kind: "container"})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write blocked by a read-only tx: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write did not complete while a read-only tx was open — the tx took the write lock")
	}
}
