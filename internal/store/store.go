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
func Open(dbPath string) (*sql.DB, error) {
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite allows only one writer at a time. Capping the pool at a single
	// connection serializes writes in Go rather than relying on busy_timeout to
	// absorb SQLITE_BUSY collisions between concurrent writers. For this app's
	// load a single connection is the simplest contention-free choice; keep it
	// open so the per-connection pragmas above are not re-established repeatedly.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(0)
	return db, nil
}
