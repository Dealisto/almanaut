// Package store handles persistence: opening the SQLite database and
// applying schema migrations.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

// Open opens (creating if needed) the SQLite database at dbPath with
// WAL journaling and foreign keys enabled.
//
// The connection pool is deliberately left at its default (multiple
// connections). WAL mode allows one writer concurrently with readers, and
// busy_timeout(5000) makes a blocked writer wait rather than fail, which is the
// standard way to handle SQLite write contention. The pool must NOT be capped
// at a single connection: the code reads from the database while a transaction
// is open on it (e.g. verifying isolation, or listing during an import), and a
// one-connection pool would deadlock — the open transaction holds the only
// connection while the concurrent read waits forever for one.
//
// Open also pings the database so an unwritable data directory or corrupt file
// surfaces immediately instead of on the first query.
func Open(dbPath string) (*sql.DB, error) {
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}
