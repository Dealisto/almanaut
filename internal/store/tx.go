package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// DBTX is the subset of *sql.DB / *sql.Tx the repositories use, so a repo can
// run either directly against the database or inside a transaction.
type DBTX interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// scanner is satisfied by both *sql.Row and *sql.Rows, letting a scanX helper
// serve both Get (single row) and List (row iteration).
type scanner interface {
	Scan(dest ...any) error
}

// boolToInt maps a Go bool to the 0/1 integer SQLite stores for it.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// WithTx runs fn inside a single transaction, committing if fn returns nil and
// rolling back if it returns an error or panics.
func WithTx(db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// sqliteBusyOrLocked reports whether err is SQLite's transient write-write
// contention signal: SQLITE_BUSY (5) or SQLITE_LOCKED (6). Those are two of
// SQLite's public result codes, and their numeric values are part of the
// documented, permanently-stable C API (https://www.sqlite.org/rescode.html)
// — a plain integer comparison is exact, so no message string matching
// ("database is locked") is needed.
//
// modernc.org/sqlite (the driver Open registers) reports both through a
// *sqlite.Error whose Code() returns sqlite3_errcode(db), the primary
// (non-extended) result code, so the plain values 5/6 are what actually
// comes back — never an extended variant like SQLITE_BUSY_SNAPSHOT that
// would need masking. Confirmed empirically: two goroutines writing
// concurrently through database/sql with busy_timeout(0) surface
// *sqlite.Error with Code()==5 unchanged, i.e. database/sql does not wrap or
// rewrite the driver's error on the way out.
//
// The check below is written against a minimal structural interface
// (Code() int) rather than the concrete *sqlite.Error type. *sqlite.Error's
// fields are unexported with no public constructor, so a test outside this
// package cannot build one to simulate contention deterministically; any
// error exposing the same Code() int method is classified identically,
// which is what lets internal/web's retry test fake this condition without
// a real locked database.
func sqliteBusyOrLocked(err error) bool {
	var coder interface{ Code() int }
	if !errors.As(err, &coder) {
		return false
	}
	const (
		sqliteBusy   = 5
		sqliteLocked = 6
	)
	code := coder.Code()
	return code == sqliteBusy || code == sqliteLocked
}

// WithTxRetry runs fn like WithTx, but retries the whole transaction — a
// fresh Begin, a fresh call to fn, and a fresh Commit — when the attempt
// fails with the transient contention sqliteBusyOrLocked recognizes.
// busy_timeout (set in Open) already makes a *blocked* writer wait for the
// lock rather than fail immediately, but it does not cover every contention
// outcome: a transaction that gets far enough to actually need the write
// lock can still lose it outright with SQLITE_BUSY or SQLITE_LOCKED,
// especially under the write pressure of a loaded CI runner.
//
// The whole closure is retried, never a single statement inside it: WithTx
// has already rolled back the failed attempt, so any decision fn made from
// its reads is stale and must be recomputed from scratch. For a caller
// racing another writer over a uniqueness decision, that recomputation is
// what turns the retry into the correct outcome — the retried attempt's read
// observes the winner's now-committed write and resolves to an ordinary,
// well-defined conflict instead of surfacing the transient failure.
//
// Bounded to 3 total attempts (1 original + 2 retries) with a short fixed
// backoff (5ms, then 10ms) between them — no jitter, since a loop this short
// and this bounded has negligible collision risk from staying in lockstep.
// Three attempts is enough for the loser of a two-way write race to succeed
// once the winner's commit lands, which normally takes well under a
// millisecond; the backoff avoids a tight spin-loop without ever holding an
// HTTP request open long enough to be perceptible (worst case ~15ms extra).
// An error that survives all 3 attempts is returned exactly as WithTx would
// return it — this bounds a real fix's blast radius, it does not paper over
// a genuine deadlock.
//
// This is a new function, not a change to WithTx: existing WithTx call sites
// keep their current fail-fast behavior unchanged.
func WithTxRetry(db *sql.DB, fn func(*sql.Tx) error) error {
	const maxAttempts = 3
	backoff := [maxAttempts - 1]time.Duration{5 * time.Millisecond, 10 * time.Millisecond}

	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = WithTx(db, fn)
		if err == nil || !sqliteBusyOrLocked(err) {
			return err
		}
		if attempt < maxAttempts-1 {
			time.Sleep(backoff[attempt])
		}
	}
	return err
}
