package store

import (
	"database/sql"
	"fmt"
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
