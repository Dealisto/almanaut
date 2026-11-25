// Package store handles persistence: opening the SQLite database and
// applying schema migrations.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

// Open opens (creating if needed) the SQLite database at dbPath with
// WAL journaling and foreign keys enabled. New persistent files are private
// (0600); permissions of existing database files are preserved.
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
// _txlock=immediate makes every read-write transaction begin as
// BEGIN IMMEDIATE, taking the write lock up front instead of trying to upgrade
// to it after its first read. That upgrade is the one contention outcome
// busy_timeout does NOT cover: SQLite refuses it immediately, without invoking
// the busy handler, because waiting could deadlock. A transaction that reads
// and then writes therefore used to fail outright the moment another
// connection committed in between — reproducibly, in well under a millisecond,
// and returned to the caller as a 500. Holding the lock from the start makes
// the contender wait on busy_timeout instead, which is what that setting is
// for. See TestWithTxReadThenWriteSurvivesContention.
//
// The cost is that a write transaction now serializes other writers for its
// whole duration, not just from its first write. For this workload — a
// homelab CMDB whose writes are small and rare — that is the right trade.
// Readers are unaffected: WAL lets them run alongside the writer.
//
// The driver only applies the mode to transactions that are not declared
// read-only, so a long read (Export reads every table for a consistent
// snapshot) must begin with sql.TxOptions{ReadOnly: true} to opt out and
// avoid holding the write lock while it works. Reach for that whenever a
// transaction genuinely only reads.
//
// Open also pings the database so an unwritable data directory or corrupt file
// surfaces immediately instead of on the first query.
func Open(dbPath string) (*sql.DB, error) {
	// A filesystem path is not a DSN: query markers in a directory or file
	// name must not truncate the path or become SQLite connection options.
	filename := dbPath
	if filename == "" {
		// An empty SQLite URI opens a temporary database. Without the URI
		// prefix, the driver treats a leading '?' as a literal filename.
		filename = "file:"
	}
	if dbPath != "" && dbPath != ":memory:" {
		absolute, err := filepath.Abs(dbPath)
		if err != nil {
			return nil, fmt.Errorf("resolve database path: %w", err)
		}
		// SQLite's default creation mode is 0644. Reserve a new file privately
		// before the driver opens it, without truncating or chmodding an
		// existing deployment. O_EXCL also makes concurrent initial opens safe.
		file, err := os.OpenFile(absolute, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil && !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create database file: %w", err)
		}
		if err == nil {
			if err := file.Close(); err != nil {
				return nil, fmt.Errorf("close database file: %w", err)
			}
		}
		path := filepath.ToSlash(absolute)
		if filepath.VolumeName(absolute) != "" && path[0] != '/' {
			path = "/" + path
		}
		filename = (&url.URL{Scheme: "file", Path: path}).String()
	}
	dsn := filename + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
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
