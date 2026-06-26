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
	return db, nil
}
