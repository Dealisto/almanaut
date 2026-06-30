package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrNotFound is returned by repository reads when no row matches the requested
// id, and by Update when it matches no row. It lets HTTP handlers distinguish a
// missing entity (404) from a real backend failure (500) instead of collapsing
// both into "not found".
var ErrNotFound = errors.New("not found")

// notFound maps a wrapped sql.ErrNoRows to ErrNotFound, leaving any other error
// (including nil) unchanged. Used by the scanX helpers so a missing row reads as
// ErrNotFound while a genuine scan failure keeps its context.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// rowsAffectedOrNotFound reports ErrNotFound when an UPDATE matched no row, so an
// edit targeting a since-deleted id surfaces as a 404 rather than a false success.
func rowsAffectedOrNotFound(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
